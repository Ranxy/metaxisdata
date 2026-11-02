package qualityrunner

import (
	"context"
	"slices"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// batchPersonalColumns caches one target's personal-data columns for the duration
// of a batch. Every check on a target runs in the same batch, so the accepted
// semantic labels are read once per target instead of once per execution.
//
// The set is the columns whose accepted label names personal data; the executor
// redacts their sampled values before the execution row is written.
type batchPersonalColumns struct {
	load     func(ctx context.Context, targetGUID string, metaType storepb.MetaType) ([]string, error)
	byTarget map[contractFilterKey][]string
}

// newBatchPersonalColumns starts an empty cache for one batch.
func newBatchPersonalColumns(load func(ctx context.Context, targetGUID string, metaType storepb.MetaType) ([]string, error)) *batchPersonalColumns {
	return &batchPersonalColumns{
		load:     load,
		byTarget: make(map[contractFilterKey][]string),
	}
}

// forTarget returns the personal-data columns of one target, reading the labels at
// most once per target per batch. A target without labels returns no columns.
func (p *batchPersonalColumns) forTarget(ctx context.Context, targetGUID string, metaType storepb.MetaType) ([]string, error) {
	key := contractFilterKey{targetGUID: targetGUID, metaType: metaType}
	if cached, ok := p.byTarget[key]; ok {
		return cached, nil
	}
	columns, err := p.load(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	p.byTarget[key] = columns
	return columns, nil
}

// loadBatchPersonalColumns reads one target's accepted personal-data labels for the
// batch cache. A nil store (the accounting tests) reads as "no personal columns".
func (r *Runner) loadBatchPersonalColumns() func(ctx context.Context, targetGUID string, metaType storepb.MetaType) ([]string, error) {
	return func(ctx context.Context, targetGUID string, metaType storepb.MetaType) ([]string, error) {
		if r.stores == nil {
			return nil, nil
		}
		guid := targetGUID
		tableType := metaType
		labels, err := r.stores.ListColumnSemanticLabels(ctx, &store.FindColumnSemanticLabelMessage{
			TableGUID: &guid,
			TableType: &tableType,
			Statuses: []storepb.QualityLabelStatus{
				storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED,
			},
		})
		if err != nil {
			return nil, err
		}
		columns := make([]string, 0, len(labels))
		for _, label := range labels {
			if label != nil && dataquality.IsPersonalSemanticType(label.Label) {
				columns = append(columns, label.ColumnName)
			}
		}
		slices.Sort(columns)
		return columns, nil
	}
}
