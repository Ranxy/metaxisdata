//go:build integration

package runner

import (
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// recommendChecksSeedDDL builds a table wide enough for every profile-derived check
// the deterministic recommendation pass proposes: a nullable email with NULLs, a
// small enumeration, a repeated decimal (a range), a repeated text with varying length,
// a unique external reference, and a recent timestamp.
func recommendChecksSeedDDL(database string) string {
	var rows strings.Builder
	statuses := []string{"paid", "open", "done"}
	for id := 1; id <= 120; id++ {
		email := fmt.Sprintf("'user%d@example.com'", id)
		if id%20 == 0 {
			email = "NULL"
		}
		if id > 1 {
			_, _ = rows.WriteString(",\n")
		}
		_, _ = fmt.Fprintf(&rows, "  (%d, %s, '%s', %.2f, REPEAT('x', %d), 'ref-%d', NOW() - INTERVAL %d MINUTE)",
			id, email, statuses[id%len(statuses)], float64(id%40)+0.5, 5+id%20, id, id)
	}
	return fmt.Sprintf(`
USE %s;
CREATE TABLE dq_recommend (
  id INT PRIMARY KEY,
  email VARCHAR(128) NULL,
  status VARCHAR(16) NOT NULL,
  amount DECIMAL(10,2) NOT NULL,
  note VARCHAR(255) NOT NULL,
  external_ref VARCHAR(32) NOT NULL,
  updated_at DATETIME NOT NULL
);
INSERT INTO dq_recommend (id, email, status, amount, note, external_ref, updated_at) VALUES
%s;
`, quoteMySQLIdentifier(database), rows.String())
}

// TestDataQualityRecommendChecksRealServerIntegration covers the deterministic
// recommendation pass end to end: a profile becomes a reviewable set of checks, a
// second pass refreshes instead of duplicating, a rejected identity stays rejected
// until it is forced, and a saved rule stops the proposal for good.
func TestDataQualityRecommendChecksRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	prefix := fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase)
	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx, recommendChecksSeedDDL(fixture.sourceDatabase)))
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)
	guid := waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_recommend")

	profiled, err := fixture.dataQuality.ProfileColumns(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ProfileColumnsRequest{
		Parent:    "workspaces/-",
		TableGuid: guid,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Len(t, profiled.Msg.GetColumnProfiles(), 7, "the seed table has seven columns")

	recommended, err := fixture.dataQuality.RecommendQualityChecks(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.RecommendQualityChecksRequest{
		Parent:    "workspaces/-",
		TableGuid: guid,
		MetaType:  v1pb.MetaType_TABLE,
		Language:  "en",
	}))
	require.NoError(t, err)
	suggestions := recommended.Msg.GetQualitySuggestions()
	require.GreaterOrEqual(t, len(suggestions), 5, "a seeded table yields a reviewable set")
	require.Equal(t, int32(len(suggestions)), recommended.Msg.GetSummary().GetCreated(), "a first pass creates every draft it queues")
	// The primary key is not proposed (the engine already enforces it), and the
	// unlabeled text column gets no length bound; both are reported as skipped.
	require.Equal(t, []string{"id", "note"}, recommended.Msg.GetSummary().GetSkippedColumns())

	seenTypes := make(map[v1pb.QualityRuleType]bool, len(suggestions))
	for _, suggestion := range suggestions {
		require.Equal(t, v1pb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT, suggestion.GetKind())
		require.Equal(t, v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION, suggestion.GetSource(),
			"a deterministic draft records the pass that produced it")
		require.Equal(t, dataquality.RecommendVersion, suggestion.GetPromptVersion())
		require.Empty(t, suggestion.GetProvider(), "a deterministic draft did not come from a model")
		require.Empty(t, suggestion.GetModel())
		require.Equal(t, guid, suggestion.GetTarget().GetMetaGuid())
		require.NotEmpty(t, suggestion.GetPayload().GetRule().GetTitle())
		require.NotEmpty(t, suggestion.GetPayload().GetRule().GetRationale())
		require.NotNil(t, suggestion.GetPayload().GetRule().GetConfig())
		seenTypes[recommendationRuleType(suggestion)] = true
	}
	for _, want := range []v1pb.QualityRuleType{
		v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		v1pb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		v1pb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
	} {
		require.Truef(t, seenTypes[want], "no %s draft was derived", want)
	}

	// The row-count floor is half of the observed 120 rows.
	rowCount := findRecommendation(suggestions, v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT)
	require.NotNil(t, rowCount)
	require.Equal(t, int64(60), rowCount.GetPayload().GetRule().GetConfig().GetRowCount().GetMinRows())
	// The freshness bound is an hour: the observed lag is a minute or two, and the pass
	// never proposes less than an hour.
	freshness := findRecommendation(suggestions, v1pb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS)
	require.NotNil(t, freshness)
	require.Equal(t, int32(3600), freshness.GetPayload().GetRule().GetConfig().GetFreshness().GetMaxLagSeconds())
	// The null-rate bound is wider than the observed 5% (6 of 120 rows).
	nullRate := findRecommendation(suggestions, v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT)
	require.NotNil(t, nullRate)
	require.Greater(t, nullRate.GetPayload().GetRule().GetConfig().GetNullPercent().GetMaxNullPct(), 5.0)

	pending := fixture.pendingRecommendations(t, guid)
	require.Len(t, pending, len(suggestions), "one live proposal per identity")

	// A second pass without force refreshes its own pending proposals in place: no new
	// rows, same identities.
	refreshed, err := fixture.dataQuality.RecommendQualityChecks(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.RecommendQualityChecksRequest{
		Parent:    "workspaces/-",
		TableGuid: guid,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Zero(t, refreshed.Msg.GetSummary().GetCreated())
	require.Equal(t, int32(len(pending)), refreshed.Msg.GetSummary().GetRefreshed())
	require.Len(t, fixture.pendingRecommendations(t, guid), len(pending))

	// A semantic label is recommendation evidence: once the text column is labeled free
	// text, the pass derives a length bound for it and stops listing it as skipped.
	_, err = fixture.dataQuality.UpdateColumnLabel(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.UpdateColumnLabelRequest{
		Parent:    "workspaces/-",
		TableGuid: guid,
		MetaType:  v1pb.MetaType_TABLE,
		Column:    "note",
		Label:     v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT,
		Action:    v1pb.ColumnLabelAction_COLUMN_LABEL_ACTION_ACCEPT,
	}))
	require.NoError(t, err)
	labeled, err := fixture.dataQuality.RecommendQualityChecks(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.RecommendQualityChecksRequest{
		Parent:    "workspaces/-",
		TableGuid: guid,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Equal(t, int32(1), labeled.Msg.GetSummary().GetCreated(), "the label enables exactly one more check")
	require.Equal(t, []string{"id"}, labeled.Msg.GetSummary().GetSkippedColumns())
	lengthDraft := findRecommendation(fixture.pendingRecommendations(t, guid), v1pb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH)
	require.NotNil(t, lengthDraft)
	require.Equal(t, "note", lengthDraft.GetPayload().GetRule().GetConfig().GetStringLength().GetColumn())

	// A rejected identity is not proposed again unless the caller forces registration.
	rejectTarget := pending[0]
	_, err = fixture.dataQuality.UpdateQualitySuggestion(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.UpdateQualitySuggestionRequest{
		Name:     rejectTarget.GetName(),
		Decision: v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED,
	}))
	require.NoError(t, err)
	afterReject, err := fixture.dataQuality.RecommendQualityChecks(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.RecommendQualityChecksRequest{
		Parent:    "workspaces/-",
		TableGuid: guid,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Zero(t, afterReject.Msg.GetSummary().GetCreated())
	require.GreaterOrEqual(t, afterReject.Msg.GetSummary().GetSkippedDecided(), int32(1))
	forced, err := fixture.dataQuality.RecommendQualityChecks(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.RecommendQualityChecksRequest{
		Parent:          "workspaces/-",
		TableGuid:       guid,
		MetaType:        v1pb.MetaType_TABLE,
		ForceRegenerate: true,
	}))
	require.NoError(t, err)
	require.GreaterOrEqual(t, forced.Msg.GetSummary().GetCreated(), int32(1), "force re-proposes the rejected identity")

	// Creating a check from a draft makes the draft a fact: the next pass skips that
	// identity instead of proposing it again.
	createFrom := findRecommendation(fixture.pendingRecommendations(t, guid), v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT)
	require.NotNil(t, createFrom)
	draft := createFrom.GetPayload().GetRule()
	created, err := fixture.dataQuality.CreateQualityRule(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.CreateQualityRuleRequest{
		Parent: "workspaces/-",
		Rule: &v1pb.QualityRule{
			Title:     draft.GetTitle(),
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			Config:    draft.GetConfig(),
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		},
		Suggestion: createFrom.GetName(),
	}))
	require.NoError(t, err)
	require.NotEmpty(t, created.Msg.GetName())
	afterCreate, err := fixture.dataQuality.RecommendQualityChecks(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.RecommendQualityChecksRequest{
		Parent:    "workspaces/-",
		TableGuid: guid,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.NoError(t, err)
	require.Zero(t, afterCreate.Msg.GetSummary().GetCreated())
	require.GreaterOrEqual(t, afterCreate.Msg.GetSummary().GetSkippedExistingFact(), int32(1))

	// A table without a profile is refused before anything is derived, pointing the
	// caller at the profile pass.
	require.NoError(t, fixture.env.ExecMySQL(fixture.ctx, fmt.Sprintf(`
USE %s;
CREATE TABLE dq_recommend_unprofiled (id INT PRIMARY KEY);
INSERT INTO dq_recommend_unprofiled (id) VALUES (1);
`, quoteMySQLIdentifier(fixture.sourceDatabase))))
	fixture.env.SyncDatabase(fixture.ctx, t, fixture.databaseName)
	unprofiledGUID := waitForMetaGUIDByName(fixture.ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_recommend_unprofiled")
	_, err = fixture.dataQuality.RecommendQualityChecks(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.RecommendQualityChecksRequest{
		Parent:    "workspaces/-",
		TableGuid: unprofiledGUID,
		MetaType:  v1pb.MetaType_TABLE,
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "ProfileColumns")
}

// pendingRecommendations lists the target's pending deterministic drafts.
func (f *qualityFixture) pendingRecommendations(t *testing.T, guid string) []*v1pb.QualitySuggestion {
	t.Helper()

	resp, err := f.dataQuality.ListQualitySuggestions(f.ctx, withToken(f.env.AdminToken(), &v1pb.ListQualitySuggestionsRequest{
		Parent:     "workspaces/-",
		PageSize:   1000,
		Status:     v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		Kind:       v1pb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		Source:     v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION,
		TargetGuid: guid,
	}))
	require.NoError(t, err)
	return resp.Msg.GetQualitySuggestions()
}

// findRecommendation returns the draft whose config carries the given rule type.
func findRecommendation(suggestions []*v1pb.QualitySuggestion, ruleType v1pb.QualityRuleType) *v1pb.QualitySuggestion {
	for _, suggestion := range suggestions {
		if recommendationRuleType(suggestion) == ruleType {
			return suggestion
		}
	}
	return nil
}

// recommendationRuleType names the rule a draft proposes from its config arm.
func recommendationRuleType(suggestion *v1pb.QualitySuggestion) v1pb.QualityRuleType {
	return qualityRuleTypeForConfig(suggestion.GetPayload().GetRule().GetConfig())
}

// qualityRuleTypeForConfig names the rule a v1 config arm carries.
func qualityRuleTypeForConfig(config *v1pb.QualityRuleConfig) v1pb.QualityRuleType {
	switch {
	case config == nil:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED
	case config.GetRowCount() != nil:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT
	case config.GetFreshness() != nil:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS
	case config.GetNullPercent() != nil:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT
	case config.GetAcceptedValues() != nil:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES
	case config.GetUnique() != nil:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE
	case config.GetRange() != nil:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_RANGE
	case config.GetStringLength() != nil:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH
	default:
		return v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED
	}
}
