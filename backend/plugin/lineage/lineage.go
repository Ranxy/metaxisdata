package lineage

import (
	"context"
	"fmt"
	"sync"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
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

var getAnalyzes = map[storepb.Engine]analyze{}

func RegisterAnalyzeRelation(engine storepb.Engine, f analyze) {
	mux.Lock()
	defer mux.Unlock()
	if _, dup := getAnalyzes[engine]; dup {
		panic(fmt.Sprintf("Register called twice %s", engine))
	}
	getAnalyzes[engine] = f
}

func GetAnalyzeRelation(ctx context.Context, engine storepb.Engine, sql string) ([]model.ColumnRelation, error) {
	mux.RLock()
	f, ok := getAnalyzes[engine]
	mux.RUnlock()
	if !ok {
		return nil, ErrorEngineNotSupported
	}
	return f(ctx, sql)
}
