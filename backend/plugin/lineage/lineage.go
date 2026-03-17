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

var (
	mux            sync.Mutex
	CatelogProvide catalog.Provide
)

func InitCatalogProvide(store *store.Store) {
	CatelogProvide = catalog.NewCatalogProvide(store)
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
	f, ok := getAnalyzes[engine]
	if !ok {
		return nil, ErrorEngineNotSupported
	}
	return f(ctx, sql)
}
