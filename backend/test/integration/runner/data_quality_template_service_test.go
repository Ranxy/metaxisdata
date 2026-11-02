//go:build integration

package runner

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// TestDataQualityRuleTemplateRealServerIntegration covers the custom-template
// lifecycle over the public API: a template saved with a column and a filter is
// stored without them, the stored parameters are what a check created from the
// template needs, and a schema-drift template is refused.
//
// The fixture carries a READ_ONLY data source because the scenario creates a real
// check from the template's config: the save path refuses a query rule on an
// instance without one (that refusal is covered elsewhere). Nothing is executed,
// so no target rows are read.
func TestDataQualityRuleTemplateRealServerIntegration(t *testing.T) {
	t.Parallel()

	fixture := setupQualityMySQLFixture(t, true)
	token := fixture.env.AdminToken()
	client := fixture.dataQuality

	createTemplate := func(request *v1pb.QualityRuleTemplate) *v1pb.QualityRuleTemplate {
		t.Helper()
		resp, err := client.CreateQualityRuleTemplate(fixture.ctx, withToken(token, &v1pb.CreateQualityRuleTemplateRequest{
			Parent:              "workspaces/-",
			QualityRuleTemplate: request,
		}))
		require.NoError(t, err, "create the quality rule template")
		return resp.Msg
	}

	// A workspace member building a template sends the editor's state, including
	// the column and filter they had picked. The server clears the target scope.
	created := createTemplate(&v1pb.QualityRuleTemplate{
		Title:       "  unique account id  ",
		Description: "  flags duplicate account identifiers  ",
		Dimension:   v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
		RuleType:    v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Severity:    v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		Config: &v1pb.QualityRuleConfig{
			Config: &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{
				Column:        "id",
				FailThreshold: proto.Float64(5),
			}},
			Filter: &v1pb.QualityFilter{Clauses: []*v1pb.QualityFilterClause{{
				Column: "id",
				Op:     v1pb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GT,
				Values: []string{"0"},
			}}},
			OnEmpty:         v1pb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS,
			TimeoutSeconds:  120,
			ThresholdMetric: v1pb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_COUNT,
		},
	})
	t.Cleanup(func() {
		// The delete step below removes it; this only cleans up after an early
		// failure. The suite shares one workspace, so a leaked template would show
		// up in other list assertions.
		_, _ = client.DeleteQualityRuleTemplate(context.Background(), withToken(token,
			&v1pb.DeleteQualityRuleTemplateRequest{Name: created.GetName()}))
	})
	require.Regexp(t, `^workspaces/[^/]+/qualityRuleTemplates/[^/]+$`, created.GetName())
	require.Equal(t, "unique account id", created.GetTitle(), "the title is trimmed")
	require.Equal(t, "flags duplicate account identifiers", created.GetDescription())
	require.Equal(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, created.GetRuleType())
	require.Empty(t, created.GetConfig().GetUnique().GetColumn(), "a template names no column")
	require.Nil(t, created.GetConfig().GetFilter(), "a template names no filter")
	require.Equal(t, 5.0, created.GetConfig().GetUnique().GetFailThreshold(), "the threshold is kept")
	require.Equal(t, v1pb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS, created.GetConfig().GetOnEmpty())
	require.Equal(t, int32(120), created.GetConfig().GetTimeoutSeconds())
	require.NotEmpty(t, created.GetCreatedBy(), "the author is recorded")
	require.NotEmpty(t, created.GetUpdateTime())

	// Get returns the stored template, not a re-derived one.
	got, err := client.GetQualityRuleTemplate(fixture.ctx, withToken(token,
		&v1pb.GetQualityRuleTemplateRequest{Name: created.GetName()}))
	require.NoError(t, err, "get the quality rule template")
	require.True(t, proto.Equal(created, got.Msg))

	// The list projects the full template, config included, newest first.
	list, err := client.ListQualityRuleTemplates(fixture.ctx, withToken(token,
		&v1pb.ListQualityRuleTemplatesRequest{Parent: "workspaces/-", PageSize: 100}))
	require.NoError(t, err, "list the quality rule templates")
	var listed *v1pb.QualityRuleTemplate
	for _, template := range list.Msg.GetQualityRuleTemplates() {
		if template.GetName() == created.GetName() {
			listed = template
			break
		}
	}
	require.NotNil(t, listed, "the created template is listed")
	require.True(t, proto.Equal(created, listed), "the list carries the same template")

	// An update replaces only the masked fields; the config is cleared again.
	updated, err := client.UpdateQualityRuleTemplate(fixture.ctx, withToken(token, &v1pb.UpdateQualityRuleTemplateRequest{
		QualityRuleTemplate: &v1pb.QualityRuleTemplate{
			Name:     created.GetName(),
			Title:    "unique account key",
			Severity: v1pb.QualitySeverity_QUALITY_SEVERITY_CRITICAL,
			Config: &v1pb.QualityRuleConfig{
				Config: &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{
					Column:        "account_id",
					FailThreshold: proto.Float64(10),
				}},
				OnEmpty: v1pb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED,
			},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title", "severity", "config"}},
	}))
	require.NoError(t, err, "update the quality rule template")
	require.Equal(t, created.GetName(), updated.Msg.GetName())
	require.Equal(t, "unique account key", updated.Msg.GetTitle())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_CRITICAL, updated.Msg.GetSeverity())
	require.Equal(t, "flags duplicate account identifiers", updated.Msg.GetDescription(), "an unmasked field is kept")
	require.Equal(t, v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS, updated.Msg.GetDimension())
	require.Empty(t, updated.Msg.GetConfig().GetUnique().GetColumn(), "an update clears the column again")
	require.Equal(t, 10.0, updated.Msg.GetConfig().GetUnique().GetFailThreshold())
	require.Equal(t, v1pb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED, updated.Msg.GetConfig().GetOnEmpty())
	require.Equal(t, created.GetCreatedBy(), updated.Msg.GetCreatedBy(), "the author is unchanged")
	require.NotEmpty(t, updated.Msg.GetUpdatedBy())

	// The returned config is usable as a check's parameters once the user fills a
	// real column: this is what "apply the template" does in the editor.
	ruleConfig, ok := proto.Clone(updated.Msg.GetConfig()).(*v1pb.QualityRuleConfig)
	require.True(t, ok)
	ruleConfig.GetUnique().Column = "id"
	rule := fixture.createRule(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		updated.Msg.GetDimension(), updated.Msg.GetSeverity(), fixture.usersGUID, ruleConfig)
	require.Equal(t, "id", rule.GetConfig().GetUnique().GetColumn())
	require.Equal(t, 10.0, rule.GetConfig().GetUnique().GetFailThreshold())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_CRITICAL, rule.GetSeverity())

	// schema_drift's expected-column snapshot is target-bound, so it cannot be a
	// template: the refusal names the reason instead of storing an unusable row.
	_, err = client.CreateQualityRuleTemplate(fixture.ctx, withToken(token, &v1pb.CreateQualityRuleTemplateRequest{
		Parent: "workspaces/-",
		QualityRuleTemplate: &v1pb.QualityRuleTemplate{
			Title:     "drift template",
			Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
			RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
			Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
			Config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_SchemaDrift{
				SchemaDrift: &v1pb.SchemaDriftConfig{},
			}},
		},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Contains(t, err.Error(), "schema drift template is not supported")

	// Deletion is physical and the template is gone afterwards.
	_, err = client.DeleteQualityRuleTemplate(fixture.ctx, withToken(token,
		&v1pb.DeleteQualityRuleTemplateRequest{Name: created.GetName()}))
	require.NoError(t, err, "delete the quality rule template")
	_, err = client.GetQualityRuleTemplate(fixture.ctx, withToken(token,
		&v1pb.GetQualityRuleTemplateRequest{Name: created.GetName()}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}
