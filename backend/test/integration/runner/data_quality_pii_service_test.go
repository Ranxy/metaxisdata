//go:build integration

package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// TestDataQualityPersonalDataRedactionRealServerIntegration proves the personal-data
// guarantee end to end: a column whose accepted label names personal data keeps no
// raw value in its cached profile and its failed-row samples, whichever check type
// produced the sample. The guarantee is enforced server-side, so a client cannot opt
// out of it.
func TestDataQualityPersonalDataRedactionRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	const seedDDL = `
CREATE TABLE dq_pii_people (
  id INT PRIMARY KEY,
  id_card VARCHAR(32) NOT NULL,
  note VARCHAR(64) NOT NULL
);
INSERT INTO dq_pii_people (id, id_card, note) VALUES
  (1, '11010519491231002X', 'valid'),
  (2, 'not-an-id', 'invalid');
`
	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx,
		"USE "+quoteMySQLIdentifier(fixture.sourceDatabase)+";"+seedDDL), "seed the PII source")
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)
	prefix := fixture.instanceID + ";" + fixture.sourceDatabase
	tableGUID := waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_pii_people")
	token := fixture.env.AdminToken()

	// Before the label, profiling observes the real values. This is the baseline the
	// label write has to redact retroactively.
	profiled, err := fixture.dataQuality.ProfileColumns(fixture.ctx, withToken(token, &v1pb.ProfileColumnsRequest{
		Parent:    "workspaces/-",
		TableGuid: tableGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Columns:   []string{"id_card"},
	}))
	require.NoError(t, err)
	require.Len(t, profiled.Msg.GetColumnProfiles(), 1)
	require.Contains(t, profiled.Msg.GetColumnProfiles()[0].GetProfile().GetMinValue(), "11010519491231",
		"the unlabeled profile is the raw observation the label must redact")

	// Accepting the label must redact the already-cached profile, not just future ones.
	_, err = fixture.dataQuality.UpdateColumnLabel(fixture.ctx, withToken(token, &v1pb.UpdateColumnLabelRequest{
		Parent:    "workspaces/-",
		TableGuid: tableGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Column:    "id_card",
		Label:     v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_RESIDENT_ID_CARD,
		Action:    v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT,
	}))
	require.NoError(t, err)

	profile := waitForColumnProfile(t, fixture, tableGUID, "id_card")
	require.NotContains(t, profile.GetProfile().GetMinValue(), "19491231", "the birth date must not stay in the cached profile")
	require.NotContains(t, profile.GetProfile().GetMaxValue(), "not-an-id", "a raw failed value must not stay in the cached profile")
	require.Contains(t, profile.GetProfile().GetMinValue(), "********")
	require.Empty(t, profile.GetProfile().GetTopValues(), "frequent values are raw values too")
	// Statistics about the column survive the redaction.
	require.Equal(t, int64(2), profile.GetProfile().GetTotalRows())

	// A failing check of another type stores a redacted sample: the label, not the
	// check type, decides that the column is personal data.
	created, err := fixture.dataQuality.CreateQualityRule(fixture.ctx, withToken(token, &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     "id card format",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			Target:    &v1pb.QualityTarget{MetaGuid: tableGUID, MetaType: v1pb.MetaType_TABLE},
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			Config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Regex{Regex: &v1pb.RegexConfig{
				Column:        "id_card",
				Pattern:       `^[0-9]{17}[0-9Xx]$`,
				FailThreshold: protoFloatPointer(0),
			}}},
		},
	}))
	require.NoError(t, err)
	executions := fixture.runRules(t, created.Msg.GetName())
	require.Len(t, executions, 1)
	finished := fixture.waitForCompletedExecution(t, executions[0].GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, finished.GetOutcome())
	require.Len(t, finished.GetSamples(), 1)
	var idCard *v1pb.ColumnValue
	for _, cell := range finished.GetSamples()[0].GetColumns() {
		if cell.GetColumn() == "id_card" {
			idCard = cell
		}
	}
	require.NotNil(t, idCard, "the checked column is part of the failed-row evidence")
	require.True(t, idCard.GetMasked(), "the sampled value must be marked as masked")
	require.NotContains(t, idCard.GetValue(), "not-an-id", "the raw failed value must not reach the execution row")
	require.Equal(t, "*********", idCard.GetValue())

	// Profiling again while the label is in effect stores a redacted profile from the
	// start (the label is read before the profile row is written).
	reprofiled, err := fixture.dataQuality.ProfileColumns(fixture.ctx, withToken(token, &v1pb.ProfileColumnsRequest{
		Parent:    "workspaces/-",
		TableGuid: tableGUID,
		MetaType:  v1pb.MetaType_TABLE,
		Columns:   []string{"id_card"},
	}))
	require.NoError(t, err)
	require.Len(t, reprofiled.Msg.GetColumnProfiles(), 1)
	require.NotContains(t, reprofiled.Msg.GetColumnProfiles()[0].GetProfile().GetMinValue(), "19491231")
	require.Contains(t, reprofiled.Msg.GetColumnProfiles()[0].GetProfile().GetMinValue(), "********")
}

// waitForColumnProfile reads one column's cached profile through the public RPC.
func waitForColumnProfile(t *testing.T, fixture *qualityFixture, tableGUID, column string) *v1pb.ColumnProfile {
	t.Helper()
	listed, err := fixture.dataQuality.ListColumnProfiles(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListColumnProfilesRequest{
		Parent:    "workspaces/-",
		TableGuid: tableGUID,
		MetaType:  v1pb.MetaType_TABLE,
		PageSize:  100,
	}))
	require.NoError(t, err)
	for _, profile := range listed.Msg.GetColumnProfiles() {
		if profile.GetColumn() == column {
			return profile
		}
	}
	t.Fatalf("no cached profile for column %q", column)
	return nil
}

// protoFloatPointer returns a pointer for an optional double field.
func protoFloatPointer(value float64) *float64 {
	return &value
}
