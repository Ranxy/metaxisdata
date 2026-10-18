package lineage

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

var (
	ErrorEngineNotSupported = errors.New("engine not supported")
)

// AnalyzerVersion identifies the analysis rules this build implements. Bump it
// whenever a change can alter analyzer output — an omni/dialect upgrade, a rule
// change, or registering a new engine. The lineage runner compares it with the
// version stored per object and re-analyzes every mismatch, so a deployment that
// carries such a change refreshes lineage by itself; no manual backfill or
// "clear column_lineage_version" step is needed.
const AnalyzerVersion = "1"

// UnsupportedStatementError reports a *partial* analysis: SQL that parsed but
// contains something the analyzer could not represent — a statement shape it does
// not model, or a reference that resolved to nothing. The relations returned
// alongside it are the ones it did find, and a caller is expected to keep them,
// while the diagnostics say what is missing so a gap never reads as "this input
// has no lineage".
//
// They are structured rather than a sentence because the causes call for different
// action: "not modelled" is a coverage gap in the analyzer, "unresolved reference"
// is usually the SQL naming something it cannot see, and a caller that only has the
// text can act on neither. Error renders them the way an operator log and a stored
// version message carry them.
//
// A parse error is not this: unparseable input yields no result at all, and an
// analyzer reports that with its own error type rather than this one.
type UnsupportedStatementError struct {
	// Diagnostics lists what the analysis could not represent, in the order it
	// found it, deduplicated and bounded.
	Diagnostics []model.Diagnostic
	// Omitted counts the diagnostics the bound left out, so the message says how
	// many it does not name instead of hiding them.
	Omitted int
}

func (e *UnsupportedStatementError) Error() string {
	return "analysis errors: " + model.FormatDiagnostics(e.Diagnostics, e.Omitted)
}

// AnalyzeRelationFunc analyzes exactly one statement and returns its column
// relations. It must not accept multi-statement input itself: the split lives
// above the dialects, or every engine ends up with its own multi-statement policy
// and they disagree — which is how one engine came to analyze a MANUAL_SQL script
// whole while another rejected it and the runner cleared the object's lineage.
type AnalyzeRelationFunc func(ctx context.Context, sql string, cat catalog.Provide) ([]model.ColumnRelation, error)

// ScriptSplitter splits a script into the statements it contains, dropping the
// ones that carry no SQL (blank lines and comments). It is a lexical scan, so it
// works on text the parser would reject; parsing stays the analyzer's job.
type ScriptSplitter func(sql string) []string

// EngineRegistration binds one engine to its single-statement analyzer and the
// splitter that feeds it one statement at a time. A dialect exports one, and the
// process assembles the whole set where it is built.
type EngineRegistration struct {
	Engine  storepb.Engine
	Analyze AnalyzeRelationFunc
	// Split may be nil for an analyzer defined for a whole script.
	Split ScriptSplitter
	// FoldIdentifierNames declares that the engine binds an identifier by its content
	// rather than by its bytes, so `users` and `Users` name one object. It is a fact
	// about the engine's SQL dialect, and the dialect is the only one that can state it:
	// T-SQL under its default collation sets it, MySQL on a case-sensitive filesystem
	// and PostgreSQL's unquoted names do not.
	//
	// The dialect's own analyzer obeys it too — the scope resolver compares names by the
	// same rule — and this field is how the layers above learn it: see
	// Analyzer.FoldsIdentifiers and Analyzer.CanonicalIdentifier.
	FoldIdentifierNames bool
}

// Analyzer resolves SQL against an explicit set of per-engine analyzers and the
// catalog they read metadata from.
//
// The set is a value the caller owns rather than package state an init function
// fills in: which engines exist is a compile-time fact, and a process that holds
// its own analyzer cannot have the registry change under it — or disagree with a
// test about what is registered.
type Analyzer struct {
	catalog catalog.Provide
	engines map[storepb.Engine]engineAnalyzer
}

// engineAnalyzer is one engine's registered pair.
type engineAnalyzer struct {
	analyze AnalyzeRelationFunc
	split   ScriptSplitter
	// foldIdentifierNames is the registration's own answer to
	// EngineRegistration.FoldIdentifierNames, kept so a caller can ask the engine's rule
	// without an analysis.
	foldIdentifierNames bool
}

// NewAnalyzer assembles an analyzer over cat and an explicit set of engines. A
// duplicate engine is a programming error in the assembly list, not a runtime
// condition: it means two dialects claim the same engine.
func NewAnalyzer(cat catalog.Provide, registrations ...EngineRegistration) *Analyzer {
	engines := make(map[storepb.Engine]engineAnalyzer, len(registrations))
	for _, registration := range registrations {
		if _, dup := engines[registration.Engine]; dup {
			panic(fmt.Sprintf("two lineage analyzers registered for %s", registration.Engine))
		}
		engines[registration.Engine] = engineAnalyzer{
			analyze:             registration.Analyze,
			split:               registration.Split,
			foldIdentifierNames: registration.FoldIdentifierNames,
		}
	}
	return &Analyzer{catalog: cat, engines: engines}
}

// Supports reports whether engine has a registered analyzer.
func (a *Analyzer) Supports(engine storepb.Engine) bool {
	_, ok := a.engines[engine]
	return ok
}

// FoldsIdentifiers reports whether engine binds an identifier case-insensitively. A
// caller that records an object a statement named asks here before comparing that name
// with the registry's own spelling; an engine that is not registered answers false,
// because nothing is known about how it names things.
func (a *Analyzer) FoldsIdentifiers(engine storepb.Engine) bool {
	return a.engines[engine].foldIdentifierNames
}

// CanonicalIdentifier returns the registry's identity for an object a statement names,
// which is the spelling the registry registered it under. A producer outside the
// analyzers — the reported-statement classifier, for one — records edges from SQL it
// parses itself, and an engine that binds identifiers case-insensitively may have it
// name `users` for the object the registry holds as `Users`; without this the edge would
// point at a GUID the registry does not have, and the graph would draw a node beside the
// object it belongs to instead of the object itself.
//
// It answers id unchanged when the engine does not fold, when the registry does not know
// the object, or when the registry could not be read: a name no one can resolve is better
// recorded as written than dropped, and the analyzer reports the catalog failure as its
// own gap.
func (a *Analyzer) CanonicalIdentifier(ctx context.Context, engine storepb.Engine, id model.ObjectIdentifier) model.ObjectIdentifier {
	if a.catalog == nil || !a.FoldsIdentifiers(engine) {
		return id
	}
	meta, err := a.catalog.GetTable(catalog.WithIdentifierFolding(ctx), id)
	if err != nil || meta == nil || meta.ID.Name == "" {
		return id
	}
	return meta.ID
}

// SplitScript splits a script into the statements it holds, using engine's splitter.
//
// Analyze splits internally, so a caller that has to see each statement on its own —
// the application-SQL ingest path fingerprints them separately — must cut the script
// exactly the same way. It asks here rather than reaching for a splitter of its own,
// because two splitters would eventually disagree about what one statement is.
//
// An unregistered engine yields nothing; an engine registered without a splitter
// yields its input whole, which is what Analyze does with it.
func (a *Analyzer) SplitScript(engine storepb.Engine, sql string) []string {
	registered, ok := a.engines[engine]
	if !ok {
		return nil
	}
	if registered.split == nil {
		return []string{sql}
	}
	return registered.split(sql)
}

// Analyze analyzes every statement in sql and returns their merged column
// relations.
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
// represent is different — its diagnostics are reported beside the edges the other
// statements produced, so a coverage gap never discards real lineage.
func (a *Analyzer) Analyze(ctx context.Context, engine storepb.Engine, sql string) ([]model.ColumnRelation, error) {
	registered, ok := a.engines[engine]
	if !ok {
		return nil, ErrorEngineNotSupported
	}
	if registered.split == nil {
		return registered.analyze(ctx, sql, a.catalog)
	}

	var (
		edges       = algorithm.NewEdgeSet()
		diagnostics []model.Diagnostic
		omitted     int
	)
	for _, statement := range registered.split(sql) {
		relations, err := registered.analyze(ctx, statement, a.catalog)
		if err != nil {
			var unsupported *UnsupportedStatementError
			if !errors.As(err, &unsupported) {
				return nil, err
			}
			diagnostics = append(diagnostics, unsupported.Diagnostics...)
			omitted += unsupported.Omitted
		}
		for _, relation := range relations {
			edges.Add(relation)
		}
	}
	if len(diagnostics) > 0 {
		merged, dropped := boundDiagnostics(diagnostics, omitted)
		return edges.Edges(), &UnsupportedStatementError{Diagnostics: merged, Omitted: dropped}
	}
	return edges.Edges(), nil
}

// gapLimit bounds how many statements' diagnostics one merged list names. A
// statement bounds its own notes the same way, but a long script would add them
// up, and the message is stored on the lineage version: a dump with hundreds of
// unsynced references must not be able to grow it without bound.
const gapLimit = 10

// boundDiagnostics keeps the first occurrence of each note and counts how many the
// limit left out.
func boundDiagnostics(notes []model.Diagnostic, omitted int) ([]model.Diagnostic, int) {
	seen := make(map[model.Diagnostic]struct{}, len(notes))
	merged := make([]model.Diagnostic, 0, min(len(notes), gapLimit))
	for _, note := range notes {
		if _, ok := seen[note]; ok {
			continue
		}
		seen[note] = struct{}{}
		if len(merged) >= gapLimit {
			omitted++
			continue
		}
		merged = append(merged, note)
	}
	return merged, omitted
}
