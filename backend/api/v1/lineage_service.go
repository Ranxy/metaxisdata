package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	v1 "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
	"github.com/pkg/errors"
)

// LineageService implements the instance service.
type LineageService struct {
	v1connect.UnimplementedLineageServiceHandler
	store        *store.Store
	stateCfg     *state.State
	dbFactory    *dbfactory.DBFactory
	schemaSyncer *schemasync.Syncer
}

// NewLineageService creates a new LineageService.
func NewLineageService(store *store.Store, stateCfg *state.State, dbFactory *dbfactory.DBFactory, schemaSyncer *schemasync.Syncer) *LineageService {
	return &LineageService{
		store:        store,
		stateCfg:     stateCfg,
		dbFactory:    dbFactory,
		schemaSyncer: schemaSyncer,
	}
}

func (s LineageService) GetLineage(ctx context.Context, req *connect.Request[v1.GetLineageRequest]) (*connect.Response[v1.GetLineageResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("metaxisdata.v1.LineageService.GetLineage is not implemented"))
}

func (s LineageService) GetLineageForContext(ctx context.Context, req *connect.Request[v1.GetLineageForContextRequest]) (*connect.Response[v1.GetLineageForContextResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("metaxisdata.v1.LineageService.GetLineageForContext is not implemented"))
}
