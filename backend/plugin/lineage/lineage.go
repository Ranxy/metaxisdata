package lineage

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

var (
	ErrorEngineNotSupported = errors.New("engine not supported")
)

// UnsupportedStatementError reports a *partial* analysis: SQL that parsed but
// contains something the analyzer could not represent — a statement shape it does
// not model, or a reference that resolved to nothing. The relations returned
// alongside it are the ones it did find, and a caller is expected to keep them,
// while the message says what is missing so a gap never reads as "this input has
// no lineage". An analyzer that reports it must classify each note, because the
// two causes call for different action: "not modelled" is a coverage gap in the
// analyzer, "unresolved reference" is usually the SQL naming something it cannot
// see.
//
// A parse error is not this: unparseable input yields no result at all, and an
// analyzer reports that with its own error type rather than this one.
type UnsupportedStatementError struct {
	Message string
}

func (e *UnsupportedStatementError) Error() string {
	return e.Message
}

var (
	mux sync.RWMutex
	// CatalogProvide is the process-wide catalog used by the registered
	// analyzers. It is set once at startup by InitCatalogProvide.
	CatalogProvide catalog.Provide
)

func InitCatalogProvide(store *store.Store) {
	mux.Lock()
	defer mux.Unlock()
	CatalogProvide = catalog.NewCatalogProvide(store)
}

// GetCatalogProvide returns the process-wide catalog provider.
func GetCatalogProvide() catalog.Provide {
	mux.RLock()
	defer mux.RUnlock()
	return CatalogProvide
}

type analyze func(ctx context.Context, sql string) ([]model.ColumnRelation, error)

// splitScript splits a script into the statements it contains, dropping the ones
// that carry no SQL (blank lines and comments). It is a lexical scan, so it works
// on text the parser would reject; parsing stays the analyzer's job.
type splitScript func(sql string) []string

var (
	getAnalyzes = map[storepb.Engine]analyze{}
	getSplits   = map[storepb.Engine]splitScript{}
)

// RegisterAnalyzeRelation registers an engine's single-statement analyzer
// together with the splitter that feeds it one statement at a time. A statement
// analyzer must not accept multi-statement input itself: the split has to live
// above the dialects, or every engine ends up with its own multi-statement policy
// and they disagree — which is how one engine came to analyze a MANUAL_SQL script
// whole while another rejected it and the runner cleared the object's lineage.
func RegisterAnalyzeRelation(engine storepb.Engine, f analyze, split splitScript) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := getAnalyzes[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	getAnalyzes[engine] = f
	getSplits[engine] = split
}

// GetAnalyzeRelation analyzes every statement in sql and returns their merged
// column relations.
//
// The script is split here, at the boundary above the dialects, because a dialect
// analyzer is defined for exactly one statement: it resolves names against a
// scope built from that one statement, and sharing the scope across statements
// made an unqualified column in a later statement resolve against an earlier
// one's relations.
//
// A statement that cannot be parsed fails the whole script, and the caller is
// expected to discard what it stored for the object: nothing about a script whose
// text does not parse can be trusted. A statement an analyzer merely cannot
// represent is different — it is reported as a gap beside the edges the other
// statements produced, so a coverage gap never discards real lineage.
func GetAnalyzeRelation(ctx context.Context, engine storepb.Engine, sql string) ([]model.ColumnRelation, error) {
	mux.RLock()
	f, ok := getAnalyzes[engine]
	split := getSplits[engine]
	mux.RUnlock()
	if !ok {
		return nil, ErrorEngineNotSupported
	}
	if split == nil {
		return f(ctx, sql)
	}

	var (
		edges = algorithm.NewEdgeSet()
		gaps  []string
	)
	for _, statement := range split(sql) {
		relations, err := f(ctx, statement)
		if err != nil {
			var unsupported *UnsupportedStatementError
			if !errors.As(err, &unsupported) {
				return nil, err
			}
			gaps = append(gaps, unsupported.Message)
		}
		for _, relation := range relations {
			edges.Add(relation)
		}
	}
	if len(gaps) > 0 {
		return edges.Edges(), &UnsupportedStatementError{Message: mergeGaps(gaps)}
	}
	return edges.Edges(), nil
}

// gapLimit bounds how many statements' gaps one merged message names. A statement
// bounds its own notes the same way, but a long script would add them up, and the
// message is stored on the lineage version: a dump with hundreds of unsynced
// references must not be able to grow it without bound.
const gapLimit = 10

// mergeGaps joins the gaps the statements reported, keeping the first occurrence
// of each and saying how many the limit left out.
func mergeGaps(gaps []string) string {
	seen := make(map[string]struct{}, len(gaps))
	merged := make([]string, 0, min(len(gaps), gapLimit))
	omitted := 0
	for _, gap := range gaps {
		if _, ok := seen[gap]; ok {
			continue
		}
		seen[gap] = struct{}{}
		if len(merged) >= gapLimit {
			omitted++
			continue
		}
		merged = append(merged, gap)
	}
	if omitted > 0 {
		merged = append(merged, fmt.Sprintf("%d more not listed", omitted))
	}
	return strings.Join(merged, "; ")
}
