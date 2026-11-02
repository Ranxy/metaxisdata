//go:build integration

package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// TestDataQualityCodeTableRealServerIntegration covers a workspace code table end to
// end: import, validate a standard-code rule against it, refuse to delete a table a
// rule still uses, and allow the delete once the rule is gone.
func TestDataQualityCodeTableRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	const seedDDL = `
CREATE TABLE dq_code_status (
  id INT PRIMARY KEY,
  status VARCHAR(16) NOT NULL
);
INSERT INTO dq_code_status (id, status) VALUES (1, '01'), (2, '02'), (3, '99');
`
	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx,
		"USE "+quoteMySQLIdentifier(fixture.sourceDatabase)+";"+seedDDL), "seed the code-table source")
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)
	prefix := fixture.instanceID + ";" + fixture.sourceDatabase
	tableGUID := waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_code_status")
	token := fixture.env.AdminToken()

	created, err := fixture.dataQuality.CreateQualityCodeTable(fixture.ctx, withToken(token, &v1pb.CreateQualityCodeTableRequest{
		Parent: "workspaces/-",
		QualityCodeTable: &v1pb.QualityCodeTable{
			Title:       "Enterprise status",
			Description: "SG 3-2021 status codes",
			Values:      []string{"02", "01", "01"},
		},
	}))
	require.NoError(t, err)
	tableName := created.Msg.GetName()
	require.NotEmpty(t, tableName)
	require.Equal(t, []string{"01", "02"}, created.Msg.GetValues(), "values are sorted and de-duplicated")
	require.Equal(t, int32(2), created.Msg.GetValueCount())

	listed, err := fixture.dataQuality.ListQualityCodeTables(fixture.ctx, withToken(token, &v1pb.ListQualityCodeTablesRequest{
		Parent:   "workspaces/-",
		PageSize: 100,
	}))
	require.NoError(t, err)
	var summary *v1pb.QualityCodeTable
	for _, candidate := range listed.Msg.GetQualityCodeTables() {
		if candidate.GetName() == tableName {
			summary = candidate
		}
	}
	require.NotNil(t, summary, "the created table is listed")
	require.Empty(t, summary.GetValues(), "a list response omits the values")
	require.Equal(t, int32(2), summary.GetValueCount())

	loaded, err := fixture.dataQuality.GetQualityCodeTable(fixture.ctx, withToken(token, &v1pb.GetQualityCodeTableRequest{Name: tableName}))
	require.NoError(t, err)
	require.Equal(t, []string{"01", "02"}, loaded.Msg.GetValues())

	// The custom-code-table standard validates membership; '99' is outside the table.
	rule, err := fixture.dataQuality.CreateQualityRule(fixture.ctx, withToken(token, &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "status must be a known code",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE,
			Target:    &v1pb.QualityTarget{MetaGuid: tableGUID, MetaType: v1pb.MetaType_TABLE},
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			Config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_StandardCode{StandardCode: &v1pb.StandardCodeConfig{
				Column:        "status",
				Standard:      v1pb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE,
				CodeTable:     tableName,
				FailThreshold: protoFloatPointer(0),
			}}},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, tableName, rule.Msg.GetConfig().GetStandardCode().GetCodeTable(),
		"the saved rule carries the canonical code-table name")

	executions := fixture.runRules(t, rule.Msg.GetName())
	require.Len(t, executions, 1)
	finished := fixture.waitForCompletedExecution(t, executions[0].GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, finished.GetOutcome())
	require.Equal(t, int64(1), finished.GetFailedRows())
	require.Equal(t, int64(3), finished.GetTotalRows())

	// Deleting the table while the rule names it is refused with an actionable error.
	_, err = fixture.dataQuality.DeleteQualityCodeTable(fixture.ctx, withToken(token, &v1pb.DeleteQualityCodeTableRequest{Name: tableName}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "referenced")

	// Removing the rule releases the reference.
	_, err = fixture.dataQuality.DeleteQualityRule(fixture.ctx, withToken(token, &v1pb.DeleteQualityRuleRequest{Name: rule.Msg.GetName()}))
	require.NoError(t, err)
	_, err = fixture.dataQuality.DeleteQualityCodeTable(fixture.ctx, withToken(token, &v1pb.DeleteQualityCodeTableRequest{Name: tableName}))
	require.NoError(t, err)
	_, err = fixture.dataQuality.GetQualityCodeTable(fixture.ctx, withToken(token, &v1pb.GetQualityCodeTableRequest{Name: tableName}))
	require.Error(t, err, "the deleted table is gone")
}
