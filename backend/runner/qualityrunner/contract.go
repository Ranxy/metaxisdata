package qualityrunner

import (
	"context"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// batchContractFilters caches one target's contract filter for the duration of a
// batch. Rules of the same table usually execute together, so the batch reads the
// contract once instead of once per execution. A metadata rule compiles no SQL, so
// it never asks for a filter: the filter would be carried but never rendered.
type batchContractFilters struct {
	load     func(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error)
	byTarget map[contractFilterKey]*storepb.QualityFilter
}

// contractFilterKey identifies one target: the GUID alone is not unique across
// object types.
type contractFilterKey struct {
	targetGUID string
	metaType   storepb.MetaType
}

// newBatchContractFilters starts an empty cache for one batch.
func newBatchContractFilters(load func(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error)) *batchContractFilters {
	return &batchContractFilters{
		load:     load,
		byTarget: make(map[contractFilterKey]*storepb.QualityFilter),
	}
}

// forRule returns the contract filter to AND into one rule's statement, reading it
// at most once per target per batch. A metadata rule (schema_drift) observes the
// registry and compiles no SQL, so it returns no filter without a store read.
func (f *batchContractFilters) forRule(ctx context.Context, ruleType storepb.QualityRuleType, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error) {
	if dataquality.IsMetadataRule(ruleType) {
		return nil, nil
	}
	key := contractFilterKey{targetGUID: targetGUID, metaType: metaType}
	if cached, ok := f.byTarget[key]; ok {
		return cached, nil
	}
	filter, err := f.load(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	f.byTarget[key] = filter
	return filter, nil
}

// loadBatchContractFilter reads one target's contract filter for the batch cache.
// A nil store (the accounting tests) reads as "no contract", and a target without a
// contract row does too.
func (r *Runner) loadBatchContractFilter() func(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error) {
	return func(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error) {
		if r.stores == nil {
			return nil, nil
		}
		contract, err := r.stores.GetQualityContract(ctx, targetGUID, metaType)
		if err != nil || contract == nil {
			return nil, err
		}
		return contract.Settings.GetFilter(), nil
	}
}
