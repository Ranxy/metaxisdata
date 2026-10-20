package v1

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityCrossTableRightGUID builds the registry GUID of a reconciliation's right
// table. The right table lives in the rule target's database (and schema, for the
// engines that have one): a qualified name may repeat that container but may not
// move to another one, because a cross-table rule compares tables that one
// connection can read together.
func qualityCrossTableRightGUID(target dataquality.ResolvedTarget, rightTable string) (string, error) {
	parts := strings.Split(strings.TrimSpace(rightTable), ".")
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return "", errors.Errorf("right_table %q has an empty segment", rightTable)
		}
	}
	if len(parts) > 2 {
		return "", errors.Errorf("right_table %q must be \"table\" or \"schema.table\"", rightTable)
	}
	database := target.Database
	schema := target.Schema
	name := strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		qualifier := strings.TrimSpace(parts[0])
		name = strings.TrimSpace(parts[1])
		if schema == "" {
			// The MySQL family qualifies with the database.
			if !strings.EqualFold(qualifier, target.Database) {
				return "", errors.Errorf("right_table %q must live in the target's database %q", rightTable, target.Database)
			}
		} else if !strings.EqualFold(qualifier, target.Schema) {
			return "", errors.Errorf("right_table %q must live in the target's schema %q", rightTable, target.Schema)
		}
	}
	return common.BuildMetaGUID(target.InstanceResourceID, database, schema, name), nil
}

// validateCrossTableRightSide resolves the right table in the registry and checks
// the right column exists and is comparable with the left one. The component
// layer validates the shapes it can see without the registry.
func (s *DataQualityService) validateCrossTableRightSide(ctx context.Context, validated *dataquality.ValidatedRule, config *storepb.QualityRuleConfig) error {
	cross := config.GetCrossTable()
	if cross == nil {
		return nil
	}
	guid, err := qualityCrossTableRightGUID(validated.Target, cross.GetRightTable())
	if err != nil {
		return connectErrorInvalid("%v", err)
	}
	right, err := dataquality.ResolveTarget(ctx, s.store, guid, storepb.MetaType_TABLE)
	if err != nil {
		if errors.Is(err, dataquality.ErrOrphaned) {
			return connectErrorInvalid("right_table %q is not in the metadata registry", cross.GetRightTable())
		}
		return err
	}
	rightFunction := cross.GetRightFunction()
	if rightFunction == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT {
		return nil
	}
	rightColumn, ok := right.Target.Column(strings.TrimSpace(cross.GetRightColumn()))
	if !ok {
		return connectErrorInvalid("right column %q does not exist on %s", cross.GetRightColumn(), right.Target.GUID)
	}
	if rightFunction == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM ||
		rightFunction == storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG {
		if rightColumn.Kind != dataquality.ColumnKindNumeric {
			return connectErrorInvalid("right column %q is %s; SUM and AVG need a numeric column", rightColumn.Name, rightColumn.Kind)
		}
	}
	leftColumn, ok := validated.Target.Column(strings.TrimSpace(cross.GetLeftColumn()))
	if ok && leftColumn.Kind != rightColumn.Kind {
		return connectErrorInvalid("left column %q is %s but right column %q is %s; they must be comparable",
			leftColumn.Name, leftColumn.Kind, rightColumn.Name, rightColumn.Kind)
	}
	return nil
}
