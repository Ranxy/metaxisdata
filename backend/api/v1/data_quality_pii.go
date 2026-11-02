package v1

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Personal data must not be persisted as evidence. A column's accepted semantic
// label decides that: when it names personal data, the cached column profile and
// every failed-row sample of that column are stored in redacted form only.
//
// The label is the one product switch here, so the redaction has to follow both
// writes that create it: profiling (before the first label can exist) and accepting
// a label (after a raw profile may already be cached).

// personalColumnsForTarget returns the columns of one target whose accepted
// semantic label names personal data. The set is keyed by the registry column
// spelling, which is also the spelling a rule config and a sample use.
func (s *DataQualityService) personalColumnsForTarget(ctx context.Context, tableGUID string, metaType storepb.MetaType) (map[string]struct{}, error) {
	guid := tableGUID
	tableType := metaType
	labels, err := s.store.ListColumnSemanticLabels(ctx, &store.FindColumnSemanticLabelMessage{
		TableGUID: &guid,
		TableType: &tableType,
		Statuses: []storepb.QualityLabelStatus{
			storepb.QualityLabelStatus_QUALITY_LABEL_STATUS_ACCEPTED,
		},
	})
	if err != nil {
		return nil, err
	}
	columns := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		if label != nil && dataquality.IsPersonalSemanticType(label.Label) {
			columns[label.ColumnName] = struct{}{}
		}
	}
	return columns, nil
}

// redactStoredProfileForPersonalColumn replaces one column's cached profile values
// with their redacted display forms. It is idempotent: a profile whose values are
// already redacted is left alone, so a repeated label write does not rewrite the
// row. A column that was never profiled is not an error.
func (s *DataQualityService) redactStoredProfileForPersonalColumn(ctx context.Context, tableGUID string, metaType storepb.MetaType, column string) error {
	profile, err := s.store.GetColumnProfile(ctx, tableGUID, metaType, column)
	if err != nil {
		return err
	}
	if profile == nil || profile.Profile == nil {
		return nil
	}
	redacted, ok := proto.Clone(profile.Profile).(*storepb.ColumnProfile)
	if !ok {
		return common.Errorf(common.Internal, "the cached profile of %q is not a column profile", column)
	}
	dataquality.RedactPersonalProfile(redacted)
	if proto.Equal(redacted, profile.Profile) {
		return nil
	}
	profile.Profile = redacted
	_, err = s.store.UpsertColumnProfile(ctx, profile)
	return err
}

// redactProfileForLabel redacts the cached profile of a column that has just been
// labeled personal data. A failure is returned to the caller: the label is already
// stored, but the profile still holds raw values, and the caller can retry the label
// write or re-profile the column.
func (s *DataQualityService) redactProfileForLabel(ctx context.Context, tableGUID string, metaType storepb.MetaType, column string, label storepb.QualitySemanticType) error {
	if !dataquality.IsPersonalSemanticType(label) {
		return nil
	}
	if err := s.redactStoredProfileForPersonalColumn(ctx, tableGUID, metaType, column); err != nil {
		return common.Errorf(common.Internal,
			"the label was stored but the cached profile of %q could not be redacted; retry the label write or re-profile the column: %v", column, err)
	}
	return nil
}
