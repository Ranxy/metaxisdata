//go:build integration

package runner

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// This scenario pins the check-type additions of P2-6 against a real server and a
// real MySQL source: allowed values can come from another column or from a
// query, a failing row-level check keeps the rows that passed beside the failed
// ones, and a segment check builds its baseline from previous executions and
// reports a segment that disappeared.

const checkTypesSeedDDL = `
CREATE TABLE dq_check_types (
  id INT PRIMARY KEY,
  status VARCHAR(16) NOT NULL,
  label VARCHAR(16) NOT NULL,
  amount INT NOT NULL
);
INSERT INTO dq_check_types (id, status, label, amount) VALUES
  (1, 'open',   'open',   50),
  (2, 'closed', 'open',   50),
  (3, 'weird',  'open',   50),
  (4, 'open',   'closed', 150);
`

func TestDataQualityCheckTypesMySQLRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx,
		"USE "+quoteMySQLIdentifier(fixture.sourceDatabase)+";"+checkTypesSeedDDL), "seed the check-types source")
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)
	prefix := fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase)
	guid := waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_check_types")

	// A reference column is the accepted set: status must appear in label, whose
	// distinct values are open and closed, so only the "weird" row fails.
	fieldRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid,
		&v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_AcceptedValues{
			AcceptedValues: &v1pb.AcceptedValuesConfig{Column: "status", Field: "label"},
		}})
	fieldExecution := fixture.runToCompletion(t, fieldRule.Name)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, fieldExecution.Outcome)
	require.Equal(t, int64(1), fieldExecution.FailedRows, "only the value outside label's set fails")
	require.Equal(t, int64(4), fieldExecution.TotalRows)
	require.Contains(t, fieldExecution.ExecutedSql, "NOT IN (SELECT")
	require.NotEmpty(t, fieldExecution.Samples, "the failing row is kept")
	require.NotEmpty(t, fieldExecution.PassingSamples, "the rows the check counted as passing are kept too")
	require.Equal(t, "weird", failureSampleValue(fieldExecution.Samples[0], "status"))

	// A query is the accepted set: only label values of the first two rows are
	// accepted, so three rows fail.
	queryRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid,
		&v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_AcceptedValues{
			AcceptedValues: &v1pb.AcceptedValuesConfig{
				Column: "status",
				Query: &v1pb.AcceptedValuesQuery{
					Sql:         "SELECT label FROM dq_check_types WHERE id <= 2",
					ValueColumn: "label",
				},
			},
		}})
	queryExecution := fixture.runToCompletion(t, queryRule.Name)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, queryExecution.Outcome)
	require.Equal(t, int64(2), queryExecution.FailedRows, "closed and weird are outside the query's set")
	require.Contains(t, queryExecution.ExecutedSql, "dq_values")
	require.NotEmpty(t, queryExecution.PassingSamples, "the accepted row is kept as the passing side")

	// A failing range check keeps the rows inside its interval beside the row
	// outside it.
	rangeRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid,
		&v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Range{Range: &v1pb.RangeConfig{
			Column: "amount",
			Min:    proto.String("0"),
			Max:    proto.String("100"),
		}}})
	rangeExecution := fixture.runToCompletion(t, rangeRule.Name)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, rangeExecution.Outcome)
	require.Equal(t, int64(1), rangeExecution.FailedRows)
	require.Len(t, rangeExecution.PassingSamples, 3, "every row inside the interval is comparison evidence")

	// A segment check records the observed segments on its first run (no baseline
	// yet), fails the segment a later run no longer has, and recovers when the
	// segment is back while the new segment stays evidence only.
	segmentRule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK,
		v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, guid,
		&v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_SegmentCheck{
			SegmentCheck: &v1pb.SegmentCheckConfig{Columns: []string{"status"}},
		}})
	firstRun := fixture.runToCompletion(t, segmentRule.Name)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, firstRun.Outcome, "the first run only records the baseline")
	require.NotNil(t, firstRun.SegmentEvidence)
	require.Len(t, firstRun.SegmentEvidence.GetObserved(), 3)
	require.Zero(t, firstRun.SegmentEvidence.GetBaselineRuns())

	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx,
		"USE "+quoteMySQLIdentifier(fixture.sourceDatabase)+";DELETE FROM dq_check_types WHERE id = 2;"),
		"drop the closed segment")
	secondRun := fixture.runToCompletion(t, segmentRule.Name)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, secondRun.Outcome)
	require.Equal(t, int64(1), secondRun.FailedRows, "the closed segment disappeared")
	require.Equal(t, int32(1), secondRun.SegmentEvidence.GetBaselineRuns())
	require.Equal(t, int64(3), secondRun.SegmentEvidence.GetExpectedCount())
	require.Len(t, secondRun.SegmentEvidence.GetDisappeared(), 1)
	require.Equal(t, "closed", secondRun.SegmentEvidence.GetDisappeared()[0].GetValues()[0])
	require.Empty(t, secondRun.SegmentEvidence.GetNewSegments())

	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx,
		"USE "+quoteMySQLIdentifier(fixture.sourceDatabase)+
			";INSERT INTO dq_check_types (id, status, label, amount) VALUES (5, 'refunded', 'open', 10);"),
		"add a new segment")
	thirdRun := fixture.runToCompletion(t, segmentRule.Name)
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_PASS, thirdRun.Outcome, "no expected segment is missing")
	require.Len(t, thirdRun.SegmentEvidence.GetNewSegments(), 1)
	require.Equal(t, "refunded", thirdRun.SegmentEvidence.GetNewSegments()[0].GetValues()[0])
	require.Empty(t, thirdRun.SegmentEvidence.GetDisappeared())
}

// failureSampleValue reads one column of a stored failure sample.
func failureSampleValue(sample *v1pb.FailureSample, column string) string {
	for _, cell := range sample.GetColumns() {
		if cell.GetColumn() == column {
			return cell.GetValue()
		}
	}
	return ""
}
