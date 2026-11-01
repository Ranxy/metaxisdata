//go:build integration

package runner

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// llmIntegrationConfig is a real model the opt-in scenario drives. It comes from the
// environment so no credential is written into the repository or the test output:
//
//	METAXISDATA_INTEGRATION_LLM_API_KEY        required; the scenario skips without it
//	METAXISDATA_INTEGRATION_LLM_BASE_URL       default https://api.deepseek.com
//	METAXISDATA_INTEGRATION_LLM_MODEL          default deepseek-flash
//	METAXISDATA_INTEGRATION_LLM_PROVIDER_TYPE  default DEEPSEEK (the v1 enum name)
type llmIntegrationConfig struct {
	apiKey       string
	baseURL      string
	model        string
	providerType v1pb.LLMProviderType
}

// loadLLMIntegrationConfig reads the opt-in provider configuration. An empty key skips
// the scenario, so the regular integration suites stay hermetic and free.
func loadLLMIntegrationConfig(t *testing.T) llmIntegrationConfig {
	t.Helper()

	apiKey := strings.TrimSpace(os.Getenv("METAXISDATA_INTEGRATION_LLM_API_KEY"))
	if apiKey == "" {
		t.Skip("set METAXISDATA_INTEGRATION_LLM_API_KEY to run the real-provider scenario")
	}
	baseURL := strings.TrimSpace(os.Getenv("METAXISDATA_INTEGRATION_LLM_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	model := strings.TrimSpace(os.Getenv("METAXISDATA_INTEGRATION_LLM_MODEL"))
	if model == "" {
		model = "deepseek-flash"
	}
	typeName := strings.TrimSpace(os.Getenv("METAXISDATA_INTEGRATION_LLM_PROVIDER_TYPE"))
	if typeName == "" {
		typeName = "LLM_PROVIDER_TYPE_DEEPSEEK"
	}
	providerType, ok := v1pb.LLMProviderType_value[typeName]
	if !ok {
		t.Fatalf("METAXISDATA_INTEGRATION_LLM_PROVIDER_TYPE=%q is not a provider type", typeName)
	}
	return llmIntegrationConfig{
		apiKey:       apiKey,
		baseURL:      baseURL,
		model:        model,
		providerType: v1pb.LLMProviderType(providerType),
	}
}

// pendingSuggestionSnapshot is one pending proposal's review-visible facts, taken
// before a second generation pass runs so the test can prove the pass left it alone.
type pendingSuggestionSnapshot struct {
	source        v1pb.QualitySuggestionSource
	promptVersion string
	config        *v1pb.QualityRuleConfig
}

// TestDataQualityLLMRecommendationPrecedenceRealServerIntegration drives a real model
// to prove the two generation passes coexist as the reference describes: the
// deterministic recommendation reads the profile, the model-assisted label pass reads
// the schema and asks the model for semantic types, and neither overwrites a live
// proposal the other produced unless the precedence rule allows it.
//
// The scenario is opt-in (see llmIntegrationConfig) and does not run in parallel: it
// creates a provider profile and flips a workspace-wide switch, restoring both first.
// The fixture makes the overlap deterministic: the recommendation holds a null-rate
// draft for the email column before the label pass runs, and the model is expected to
// label that column with a format type, so the label pass must skip its null-rate draft
// and queue its default-pattern regex draft instead. Accepting that label then lets the
// recommendation replace the label pass's default regex draft, and only in that
// direction.
func TestDataQualityLLMRecommendationPrecedenceRealServerIntegration(t *testing.T) {
	config := loadLLMIntegrationConfig(t)

	fixture := setupQualityMySQLFixture(t, true)
	ctx := fixture.ctx
	token := fixture.env.AdminToken()
	// The label pass waits on a model; the fixture's client timeout is too short for a
	// reasoning model writing a JSON answer for seven columns.
	client := v1connect.NewDataQualityServiceClient(&http.Client{Timeout: 5 * time.Minute}, fixture.env.BaseURL)
	llmClient := v1connect.NewLLMServiceClient(&http.Client{Timeout: 60 * time.Second}, fixture.env.BaseURL)

	// The workspace switch is global, so this scenario owns it while it runs.
	updateLLMAnalysis := func(enabled bool) {
		t.Helper()
		var dataQuality *v1pb.DataQualitySetting
		if enabled {
			dataQuality = &v1pb.DataQualitySetting{EnableLlmAnalysis: true}
		}
		_, err := fixture.settings.UpdateWorkspaceProfileSetting(ctx, withToken(token, &v1pb.UpdateWorkspaceProfileSettingRequest{
			Setting:    &v1pb.WorkspaceProfileSetting{DataQuality: dataQuality},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"data_quality"}},
		}))
		require.NoError(t, err)
	}
	updateLLMAnalysis(true)
	t.Cleanup(func() { updateLLMAnalysis(false) })

	profile, err := llmClient.CreateLLMProviderProfile(ctx, withToken(token, &v1pb.CreateLLMProviderProfileRequest{
		Profile: &v1pb.LlmProviderProfile{
			Title:   "integration real provider",
			Type:    config.providerType,
			BaseUrl: config.baseURL,
			ApiKey:  config.apiKey,
			Models:  []*v1pb.LlmProviderModel{{Name: config.model, Enabled: true}},
		},
	}))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = llmClient.DeleteLLMProviderProfile(context.Background(), withToken(token, &v1pb.DeleteLLMProviderProfileRequest{Name: profile.Msg.GetName()}))
	})

	// A table whose columns the model can label unambiguously and whose profile gives the
	// deterministic pass something to hold on every one of them.
	prefix := fmt.Sprintf("%s;%s", fixture.instanceID, fixture.sourceDatabase)
	require.NoError(t, fixture.env.ExecMySQL(ctx, recommendChecksSeedDDL(fixture.sourceDatabase)))
	fixture.env.SyncDatabase(ctx, t, fixture.databaseName)
	guid := waitForMetaGUIDByName(ctx, t, fixture.env, prefix, storepb.MetaType_TABLE, "dq_recommend")
	columns := []string{"id", "email", "status", "amount", "note", "external_ref", "updated_at"}

	// Warm the profile the recommendation pass reads.
	_, err = client.ProfileColumns(ctx, withToken(token, &v1pb.ProfileColumnsRequest{
		Parent: "workspaces/-", TableGuid: guid, MetaType: v1pb.MetaType_TABLE, Columns: columns,
	}))
	require.NoError(t, err)

	recommended, err := client.RecommendQualityChecks(ctx, withToken(token, &v1pb.RecommendQualityChecksRequest{
		Parent: "workspaces/-", TableGuid: guid, MetaType: v1pb.MetaType_TABLE, Columns: columns, Language: "en",
	}))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(recommended.Msg.GetQualitySuggestions()), 5,
		"the profile yields a reviewable recommendation set: %v", recommended.Msg.GetSummary())
	before := snapshotPendingSuggestions(ctx, t, client, token, guid, v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION)
	require.NotEmpty(t, before)
	recommendationTypes := recommendationTypesByColumn(before)

	// The real model labels the columns. This is the end-to-end proof that the pass runs
	// against a provider at all.
	labeled, err := client.SuggestColumnLabels(ctx, withToken(token, &v1pb.SuggestColumnLabelsRequest{
		Parent: "workspaces/-", TableGuid: guid, MetaType: v1pb.MetaType_TABLE, Columns: columns, Language: "en",
	}))
	require.NoErrorf(t, err, "the real provider answered the label pass")
	require.GreaterOrEqual(t, labeled.Msg.GetSummary().GetCreated(), int32(1), "the model proposed something: %v", labeled.Msg.GetSummary())
	labels := filterSuggestions(labeled.Msg.GetQualitySuggestions(), v1pb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL)
	require.NotEmpty(t, labels, "the model labeled at least one column")

	// Direction one: the label pass must not put its default parameters back over a live
	// recommendation, so every recommendation draft keeps its payload and source.
	after := snapshotPendingSuggestions(ctx, t, client, token, guid, v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION)
	require.Len(t, after, len(before), "no recommendation draft was closed by the label pass")
	for name, snapshot := range before {
		current, ok := after[name]
		require.Truef(t, ok, "recommendation draft %s disappeared", name)
		require.Equalf(t, snapshot.source, current.source, "recommendation %s changed source", name)
		require.Equalf(t, snapshot.promptVersion, current.promptVersion, "recommendation %s changed pass version", name)
		require.Truef(t, proto.Equal(snapshot.config, current.config),
			"recommendation %s payload changed:\nbefore %v\nafter  %v", name, snapshot.config, current.config)
	}

	// The overlap is what makes direction one testable: at least one label maps to a rule
	// the recommendation already holds, and the label pass reports it as skipped.
	overlaps := false
	for _, suggestion := range labels {
		label := suggestion.GetPayload().GetLabel()
		for _, ruleType := range dataquality.RuleTypesForSemantic(storepb.QualitySemanticType(label.GetLabel())) {
			if recommendationTypes[label.GetColumn()][ruleType] {
				overlaps = true
			}
		}
	}
	require.True(t, overlaps, "the model should label a column whose check the recommendation already holds")
	require.GreaterOrEqual(t, labeled.Msg.GetSummary().GetSkippedPending(), int32(1),
		"the label pass skipped at least one identity the recommendation holds: %v", labeled.Msg.GetSummary())

	// Direction two: accepting a format label makes the recommendation able to replace
	// the label pass's default-pattern draft, in place and only in that direction.
	formatLabel := formatLabelSuggestion(labels)
	require.NotNil(t, formatLabel, "the model should recognize the email column as a format type")
	_, err = client.UpdateQualitySuggestion(ctx, withToken(token, &v1pb.UpdateQualitySuggestionRequest{
		Name: formatLabel.GetName(), Decision: v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED,
	}))
	require.NoError(t, err)

	defaultDraft := pendingRegexDraft(t, fixture, guid, formatLabel.GetPayload().GetLabel().GetColumn())
	require.NotNil(t, defaultDraft, "the label pass queued its default-pattern regex draft")
	require.Equal(t, v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE, defaultDraft.GetSource())

	refreshed, err := client.RecommendQualityChecks(ctx, withToken(token, &v1pb.RecommendQualityChecksRequest{
		Parent: "workspaces/-", TableGuid: guid, MetaType: v1pb.MetaType_TABLE, Columns: columns, Language: "en",
	}))
	require.NoError(t, err)
	require.GreaterOrEqual(t, refreshed.Msg.GetSummary().GetRefreshed(), int32(1),
		"the recommendation refreshed the default-parameter draft: %v", refreshed.Msg.GetSummary())

	upgraded, err := client.GetQualitySuggestion(ctx, withToken(token, &v1pb.GetQualitySuggestionRequest{Name: defaultDraft.GetName()}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION, upgraded.Msg.GetSource(),
		"the recommendation replaced the label pass's default draft")
	require.Equal(t, dataquality.RecommendVersion, upgraded.Msg.GetPromptVersion())
	require.NotNil(t, upgraded.Msg.GetPayload().GetRule().GetConfig().GetRegex())

	// Nothing above consumed a model call for a column, so a second label pass must still
	// leave the refreshed recommendation alone (the accepted label already skips its
	// column; the remaining columns exercise the skip path again).
	again, err := client.SuggestColumnLabels(ctx, withToken(token, &v1pb.SuggestColumnLabelsRequest{
		Parent: "workspaces/-", TableGuid: guid, MetaType: v1pb.MetaType_TABLE,
		Columns: []string{"status", "note", "amount", "updated_at"}, Language: "en",
	}))
	require.NoError(t, err)
	require.GreaterOrEqual(t, again.Msg.GetSummary().GetSkippedPending(), int32(1),
		"the second label pass skipped recommendations it may not overwrite: %v", again.Msg.GetSummary())
	stillRecommended, err := client.GetQualitySuggestion(ctx, withToken(token, &v1pb.GetQualitySuggestionRequest{Name: defaultDraft.GetName()}))
	require.NoError(t, err)
	require.Equal(t, v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_RECOMMENDATION, stillRecommended.Msg.GetSource())
}

// snapshotPendingSuggestions reads one source's pending drafts as a map by resource name.
func snapshotPendingSuggestions(ctx context.Context, t *testing.T, client v1connect.DataQualityServiceClient, token, guid string, source v1pb.QualitySuggestionSource) map[string]pendingSuggestionSnapshot {
	t.Helper()

	resp, err := client.ListQualitySuggestions(ctx, withToken(token, &v1pb.ListQualitySuggestionsRequest{
		Parent:     "workspaces/-",
		PageSize:   1000,
		Status:     v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		Kind:       v1pb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		Source:     source,
		TargetGuid: guid,
	}))
	require.NoError(t, err)
	snapshots := make(map[string]pendingSuggestionSnapshot, len(resp.Msg.GetQualitySuggestions()))
	for _, suggestion := range resp.Msg.GetQualitySuggestions() {
		snapshot := pendingSuggestionSnapshot{
			source:        suggestion.GetSource(),
			promptVersion: suggestion.GetPromptVersion(),
		}
		if config := suggestion.GetPayload().GetRule().GetConfig(); config != nil {
			if cloned, ok := proto.Clone(config).(*v1pb.QualityRuleConfig); ok {
				snapshot.config = cloned
			}
		}
		snapshots[suggestion.GetName()] = snapshot
	}
	return snapshots
}

// recommendationTypesByColumn indexes the rule types a recommendation holds per column.
func recommendationTypesByColumn(snapshots map[string]pendingSuggestionSnapshot) map[string]map[storepb.QualityRuleType]bool {
	types := make(map[string]map[storepb.QualityRuleType]bool)
	for _, snapshot := range snapshots {
		column := qualityConfigColumn(snapshot.config)
		if column == "" {
			continue
		}
		if types[column] == nil {
			types[column] = make(map[storepb.QualityRuleType]bool)
		}
		if ruleType := qualityRuleTypeForConfig(snapshot.config); ruleType != v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
			types[column][storepb.QualityRuleType(ruleType)] = true
		}
	}
	return types
}

// qualityConfigColumn names the single column a v1 config arm observes, or "" for a
// table-level rule. It mirrors dataquality.ConfigColumn for the API-shaped config the
// test reads back.
func qualityConfigColumn(config *v1pb.QualityRuleConfig) string {
	switch {
	case config.GetNullPercent() != nil:
		return config.GetNullPercent().GetColumn()
	case config.GetAcceptedValues() != nil:
		return config.GetAcceptedValues().GetColumn()
	case config.GetUnique() != nil:
		return config.GetUnique().GetColumn()
	case config.GetRange() != nil:
		return config.GetRange().GetColumn()
	case config.GetRegex() != nil:
		return config.GetRegex().GetColumn()
	case config.GetStringLength() != nil:
		return config.GetStringLength().GetColumn()
	case config.GetFreshness() != nil:
		return config.GetFreshness().GetTimestampColumn()
	case config.GetReferentialIntegrity() != nil:
		return config.GetReferentialIntegrity().GetChildColumn()
	default:
		return ""
	}
}

// filterSuggestions keeps the suggestions of one kind.
func filterSuggestions(suggestions []*v1pb.QualitySuggestion, kind v1pb.QualitySuggestionKind) []*v1pb.QualitySuggestion {
	kept := make([]*v1pb.QualitySuggestion, 0, len(suggestions))
	for _, suggestion := range suggestions {
		if suggestion.GetKind() == kind {
			kept = append(kept, suggestion)
		}
	}
	return kept
}

// formatLabelSuggestion returns the first label suggestion whose semantic type has a
// deterministic format pattern, or nil.
func formatLabelSuggestion(labels []*v1pb.QualitySuggestion) *v1pb.QualitySuggestion {
	formatTypes := map[v1pb.QualitySemanticType]bool{
		v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL: true,
		v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_PHONE: true,
		v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_URL:   true,
		v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UUID:  true,
		v1pb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_IP:    true,
	}
	for _, suggestion := range labels {
		if formatTypes[suggestion.GetPayload().GetLabel().GetLabel()] {
			return suggestion
		}
	}
	return nil
}

// pendingRegexDraft finds the label pass's pending regex draft for one column.
func pendingRegexDraft(t *testing.T, fixture *qualityFixture, guid, column string) *v1pb.QualitySuggestion {
	t.Helper()

	resp, err := fixture.dataQuality.ListQualitySuggestions(fixture.ctx, withToken(fixture.env.AdminToken(), &v1pb.ListQualitySuggestionsRequest{
		Parent:     "workspaces/-",
		PageSize:   1000,
		Status:     v1pb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		Kind:       v1pb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
		Source:     v1pb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_PROFILE,
		TargetGuid: guid,
	}))
	require.NoError(t, err)
	for _, suggestion := range resp.Msg.GetQualitySuggestions() {
		config := suggestion.GetPayload().GetRule().GetConfig()
		if config.GetRegex() != nil && config.GetRegex().GetColumn() == column {
			return suggestion
		}
	}
	return nil
}
