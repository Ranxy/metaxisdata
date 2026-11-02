package v1

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestParseQualityRuleTemplateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resource  string
		wantOwner string
		wantID    string
		wantError bool
	}{
		{
			name:      "canonical name",
			resource:  "workspaces/ws-1/qualityRuleTemplates/template-1",
			wantOwner: "ws-1",
			wantID:    "template-1",
		},
		{
			name:      "current workspace shorthand",
			resource:  "workspaces/-/qualityRuleTemplates/template-1",
			wantOwner: "-",
			wantID:    "template-1",
		},
		{name: "wrong collection", resource: "workspaces/ws-1/qualityRules/template-1", wantError: true},
		{name: "missing id", resource: "workspaces/ws-1/qualityRuleTemplates/", wantError: true},
		{name: "extra segment", resource: "workspaces/ws-1/qualityRuleTemplates/template-1/extra", wantError: true},
		{name: "empty", resource: "", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			owner, id, err := parseQualityRuleTemplateName(test.resource)
			if test.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantOwner, owner)
			require.Equal(t, test.wantID, id)
		})
	}
}

func TestFormatQualityRuleTemplateNameRoundTrips(t *testing.T) {
	t.Parallel()

	name := formatQualityRuleTemplateName("ws-1", "template-1")
	require.Equal(t, "workspaces/ws-1/qualityRuleTemplates/template-1", name)

	workspace, resourceID, err := parseQualityRuleTemplateName(name)
	require.NoError(t, err)
	require.Equal(t, "ws-1", workspace)
	require.Equal(t, "template-1", resourceID)
}

// validQualityTemplateRequest builds a create request carrying every kind of
// target scope a user's editor state can include.
func validQualityTemplateRequest() *v1pb.QualityRuleTemplate {
	return &v1pb.QualityRuleTemplate{
		Title:       "  unique account id  ",
		Description: "  flags duplicate account identifiers  ",
		Dimension:   v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
		RuleType:    v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Severity:    v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		Config: &v1pb.QualityRuleConfig{
			Config: &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{
				Column:        "account_id",
				FailThreshold: proto.Float64(5),
			}},
			Filter: &v1pb.QualityFilter{Clauses: []*v1pb.QualityFilterClause{{
				Column: "account_id",
				Op:     v1pb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GT,
				Values: []string{"0"},
			}}},
			OnEmpty:        v1pb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS,
			TimeoutSeconds: 60,
			Sampling:       &v1pb.QualityRuleSampling{Method: &v1pb.QualityRuleSampling_MaxRows{MaxRows: 1000}},
		},
	}
}

// A template is stored without a target: the create path clears the column and
// filter the editor sent while keeping the parameters that carry the rule's
// meaning.
func TestQualityTemplateFromCreateClearsTargetScope(t *testing.T) {
	t.Parallel()

	message, err := qualityTemplateFromCreate(validQualityTemplateRequest())
	require.NoError(t, err)

	require.Equal(t, "unique account id", message.Title, "the title is trimmed")
	require.Equal(t, "flags duplicate account identifiers", message.Description)
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS, message.Dimension)
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, message.RuleType)
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_WARNING, message.Severity)
	require.Empty(t, message.Config.GetUnique().GetColumn(), "the checked column is target-bound")
	require.Nil(t, message.Config.GetFilter(), "the filter is target-bound")
	require.Equal(t, 5.0, message.Config.GetUnique().GetFailThreshold(), "thresholds are kept")
	require.Equal(t, storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS, message.Config.GetOnEmpty())
	require.Equal(t, int32(60), message.Config.GetTimeoutSeconds())
	require.Equal(t, int64(1000), message.Config.GetSampling().GetMaxRows())
	require.Zero(t, message.ResourceID, "the server assigns the resource id")
}

func TestQualityTemplateFromCreateRefusals(t *testing.T) {
	t.Parallel()

	withTitle := func(title string) *v1pb.QualityRuleTemplate {
		request := validQualityTemplateRequest()
		request.Title = title
		return request
	}
	withDescription := func(description string) *v1pb.QualityRuleTemplate {
		request := validQualityTemplateRequest()
		request.Description = description
		return request
	}
	withConfig := func(config *v1pb.QualityRuleConfig) *v1pb.QualityRuleTemplate {
		request := validQualityTemplateRequest()
		request.Config = config
		return request
	}

	tests := []struct {
		name      string
		request   *v1pb.QualityRuleTemplate
		wantError string
	}{
		{name: "missing template", request: nil, wantError: "the quality rule template is required"},
		{name: "blank title", request: withTitle("   "), wantError: "title is required"},
		{name: "title at the limit", request: withTitle(strings.Repeat("a", qualityTemplateTitleMaxRunes))},
		{name: "title above the limit", request: withTitle(strings.Repeat("a", qualityTemplateTitleMaxRunes+1)),
			wantError: "title must be at most 200 characters"},
		{name: "description at the limit", request: withDescription(strings.Repeat("d", qualityTemplateDescriptionMaxRunes))},
		{name: "description above the limit", request: withDescription(strings.Repeat("d", qualityTemplateDescriptionMaxRunes+1)),
			wantError: "description must be at most 2000 characters"},
		{name: "multibyte title is counted in runes", request: withTitle(strings.Repeat("检", qualityTemplateTitleMaxRunes))},
		{name: "unspecified dimension", request: func() *v1pb.QualityRuleTemplate {
			request := validQualityTemplateRequest()
			request.Dimension = v1pb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED
			return request
		}(), wantError: "dimension is required"},
		{name: "unspecified severity", request: func() *v1pb.QualityRuleTemplate {
			request := validQualityTemplateRequest()
			request.Severity = v1pb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED
			return request
		}(), wantError: "severity is required"},
		{name: "missing config", request: withConfig(nil), wantError: "config is required"},
		{name: "schema drift is not templatable", request: func() *v1pb.QualityRuleTemplate {
			request := validQualityTemplateRequest()
			request.RuleType = v1pb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT
			request.Config = &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_SchemaDrift{
				SchemaDrift: &v1pb.SchemaDriftConfig{},
			}}
			return request
		}(), wantError: "schema drift template is not supported"},
		{name: "config arm mismatch", request: func() *v1pb.QualityRuleTemplate {
			request := validQualityTemplateRequest()
			request.Config = &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{
				RowCount: &v1pb.RowCountConfig{MinRows: proto.Int64(1)},
			}}
			return request
		}(), wantError: "not for the template's"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := qualityTemplateFromCreate(test.request)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), test.wantError)
		})
	}
}

func storedQualityTemplate() *store.QualityRuleTemplateMessage {
	return &store.QualityRuleTemplateMessage{
		ID:          9,
		ResourceID:  "template-1",
		Title:       "unique account id",
		Description: "flags duplicate account identifiers",
		Dimension:   storepb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
		RuleType:    storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Severity:    storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		Config: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{
				FailThreshold: proto.Float64(5),
			}},
		},
		CreatedBy: proto.Int32(3),
	}
}

func TestApplyQualityRuleTemplateUpdateMask(t *testing.T) {
	t.Parallel()

	updated, err := applyQualityRuleTemplateUpdate(storedQualityTemplate(), &v1pb.QualityRuleTemplate{
		Title:    "  unique account key  ",
		Severity: v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
	}, []string{"title", "severity"})
	require.NoError(t, err)
	require.Equal(t, "unique account key", updated.Title, "the title is trimmed")
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, updated.Severity)
	require.Equal(t, "flags duplicate account identifiers", updated.Description, "an unmasked field is kept")
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS, updated.Dimension)
	require.Equal(t, 5.0, updated.Config.GetUnique().GetFailThreshold(), "an unmasked config is kept")
	require.Equal(t, "template-1", updated.ResourceID)

	// An empty mask replaces every mutable field, which is what AIP-134 says.
	full, err := applyQualityRuleTemplateUpdate(storedQualityTemplate(), &v1pb.QualityRuleTemplate{
		Title:       "new title",
		Description: "new description",
		Severity:    v1pb.QualitySeverity_QUALITY_SEVERITY_CRITICAL,
		Dimension:   v1pb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY,
		Config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{
			Column:        "account_id",
			FailThreshold: proto.Float64(9),
		}}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "new title", full.Title)
	require.Equal(t, "new description", full.Description)
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL, full.Severity)
	require.Equal(t, storepb.QualityDimension_QUALITY_DIMENSION_CONSISTENCY, full.Dimension)
	require.Equal(t, 9.0, full.Config.GetUnique().GetFailThreshold())
	require.Empty(t, full.Config.GetUnique().GetColumn(), "the update clears the column too")
}

func TestApplyQualityRuleTemplateUpdateRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		current   *store.QualityRuleTemplateMessage
		request   *v1pb.QualityRuleTemplate
		mask      []string
		wantError string
	}{
		{name: "missing current", current: nil, request: &v1pb.QualityRuleTemplate{}, wantError: "the quality rule template is required"},
		{name: "missing request", current: storedQualityTemplate(), request: nil, wantError: "the quality rule template is required"},
		{name: "unknown path", current: storedQualityTemplate(), request: &v1pb.QualityRuleTemplate{Title: "t"},
			mask: []string{"rule_type"}, wantError: `unsupported update path "rule_type"`},
		{name: "blank masked title", current: storedQualityTemplate(), request: &v1pb.QualityRuleTemplate{Title: " "},
			mask: []string{"title"}, wantError: "title is required"},
		{name: "masked severity is unset", current: storedQualityTemplate(), request: &v1pb.QualityRuleTemplate{},
			mask: []string{"severity"}, wantError: "severity is required"},
		{name: "masked dimension is unset", current: storedQualityTemplate(), request: &v1pb.QualityRuleTemplate{},
			mask: []string{"dimension"}, wantError: "dimension is required"},
		{name: "masked config is missing", current: storedQualityTemplate(), request: &v1pb.QualityRuleTemplate{},
			mask: []string{"config"}, wantError: "config is required"},
		{name: "masked config has invalid thresholds", current: storedQualityTemplate(), request: &v1pb.QualityRuleTemplate{
			Config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_Unique{Unique: &v1pb.UniqueConfig{
				FailThreshold: proto.Float64(-1),
			}}},
		}, mask: []string{"config"}, wantError: "must not be negative"},
		{name: "masked config changes the arm", current: storedQualityTemplate(), request: &v1pb.QualityRuleTemplate{
			Config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_RowCount{
				RowCount: &v1pb.RowCountConfig{MinRows: proto.Int64(1)},
			}},
		}, mask: []string{"config"}, wantError: "not for the template's"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := applyQualityRuleTemplateUpdate(test.current, test.request, test.mask)
			require.Error(t, err)
			require.Contains(t, err.Error(), test.wantError)
		})
	}
}

func TestConvertQualityRuleTemplateToV1(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.November, 2, 9, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	message := storedQualityTemplate()
	message.CreatedAt = createdAt
	message.UpdatedAt = updatedAt
	message.UpdatedBy = proto.Int32(4)

	converted := convertQualityRuleTemplateToV1("ws-1", message)
	require.Equal(t, "workspaces/ws-1/qualityRuleTemplates/template-1", converted.GetName())
	require.Equal(t, "unique account id", converted.GetTitle())
	require.Equal(t, v1pb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, converted.GetRuleType())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING, converted.GetSeverity())
	require.Equal(t, v1pb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS, converted.GetDimension())
	require.Equal(t, 5.0, converted.GetConfig().GetUnique().GetFailThreshold())
	require.Equal(t, createdAt, converted.GetCreateTime().AsTime())
	require.Equal(t, updatedAt, converted.GetUpdateTime().AsTime())
	require.Equal(t, "users/3", converted.GetCreatedBy())
	require.Equal(t, "users/4", converted.GetUpdatedBy())

	require.Nil(t, convertQualityRuleTemplateToV1("ws-1", nil))
}
