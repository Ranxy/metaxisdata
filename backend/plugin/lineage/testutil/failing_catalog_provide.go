package testutil

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// FailingCatalogProvide fails every lookup, so a test can exercise the path where
// an analysis degrades because the catalog could not answer.
type FailingCatalogProvide struct {
	err error
}

// NewFailingCatalogProvide returns a provider whose every lookup fails.
func NewFailingCatalogProvide(err error) *FailingCatalogProvide {
	return &FailingCatalogProvide{err: err}
}

func (p *FailingCatalogProvide) GetTable(_ context.Context, _ model.ObjectIdentifier) (*catalog.TableMeta, error) {
	return nil, p.err
}
