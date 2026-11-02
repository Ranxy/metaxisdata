package v1

import (
	"context"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// qualityTemplatePrefix is the collection segment of a template resource name.
	qualityTemplatePrefix = "qualityRuleTemplates/"
	// qualityTemplateTitleMaxRunes and qualityTemplateDescriptionMaxRunes bound the
	// display text a template carries. They are rune counts, so a title in a
	// multi-byte script is not cut in half.
	qualityTemplateTitleMaxRunes       = 200
	qualityTemplateDescriptionMaxRunes = 2000
)

// parseQualityRuleTemplateName splits
// workspaces/{workspace}/qualityRuleTemplates/{quality_rule_template}.
func parseQualityRuleTemplateName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityTemplatePrefix)
	if err != nil {
		return "", "", errors.Wrap(err, "invalid quality rule template name")
	}
	return tokens[0], tokens[1], nil
}

// formatQualityRuleTemplateName renders a template resource name.
func formatQualityRuleTemplateName(workspace, qualityRuleTemplate string) string {
	return common.FormatWorkspace(workspace) + "/" + qualityTemplatePrefix + qualityRuleTemplate
}

// templateResourceID parses a template name and proves its workspace. The name
// segment may be "workspaces/-", the current-workspace shorthand the SPA uses for
// a parent; a template name always resolves to the deployment's one workspace.
func (s *DataQualityService) templateResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualityRuleTemplateName(name)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	if workspace == "-" {
		workspaceID, err := s.store.GetWorkspaceID(ctx)
		if err != nil {
			return "", "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve the workspace"))
		}
		return workspaceID, resourceID, nil
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
}

// convertQualityRuleTemplateToV1 maps one stored template. The config keeps the
// target-free parameters and carries no column or filter.
func convertQualityRuleTemplateToV1(workspaceID string, template *store.QualityRuleTemplateMessage) *v1pb.QualityRuleTemplate {
	if template == nil {
		return nil
	}
	return &v1pb.QualityRuleTemplate{
		Name:        formatQualityRuleTemplateName(workspaceID, template.ResourceID),
		Title:       template.Title,
		Description: template.Description,
		RuleType:    v1pb.QualityRuleType(template.RuleType),
		Config:      convertQualityRuleConfigToV1(template.Config),
		Severity:    v1pb.QualitySeverity(template.Severity),
		Dimension:   v1pb.QualityDimension(template.Dimension),
		CreateTime:  timestamppb.New(template.CreatedAt),
		UpdateTime:  timestamppb.New(template.UpdatedAt),
		CreatedBy:   formatQualityOwner(template.CreatedBy),
		UpdatedBy:   formatQualityOwner(template.UpdatedBy),
	}
}

// qualityTemplateTitle trims and bounds a template's title. Every template needs
// one: the list is browsed by title, so an empty row is unidentifiable.
func qualityTemplateTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return "", connectErrorInvalid("title is required")
	}
	if utf8.RuneCountInString(trimmed) > qualityTemplateTitleMaxRunes {
		return "", connectErrorInvalid("title must be at most %d characters", qualityTemplateTitleMaxRunes)
	}
	return trimmed, nil
}

// qualityTemplateDescription trims and bounds a template's description.
func qualityTemplateDescription(description string) (string, error) {
	trimmed := strings.TrimSpace(description)
	if utf8.RuneCountInString(trimmed) > qualityTemplateDescriptionMaxRunes {
		return "", connectErrorInvalid("description must be at most %d characters", qualityTemplateDescriptionMaxRunes)
	}
	return trimmed, nil
}

// qualityTemplateFromCreate validates a create request and converts it to the
// store message. The target scope is cleared first, so a request's columns and filter
// are dropped rather than stored; validation then checks the remaining,
// target-independent shape and refuses a type that cannot be templated.
func qualityTemplateFromCreate(request *v1pb.QualityRuleTemplate) (*store.QualityRuleTemplateMessage, error) {
	if request == nil {
		return nil, connectErrorInvalid("the quality rule template is required")
	}
	title, err := qualityTemplateTitle(request.GetTitle())
	if err != nil {
		return nil, err
	}
	description, err := qualityTemplateDescription(request.GetDescription())
	if err != nil {
		return nil, err
	}
	dimension := storepb.QualityDimension(request.GetDimension())
	if dimension == storepb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED {
		return nil, connectErrorInvalid("dimension is required")
	}
	severity := storepb.QualitySeverity(request.GetSeverity())
	if severity == storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED {
		return nil, connectErrorInvalid("severity is required")
	}
	ruleType := storepb.QualityRuleType(request.GetRuleType())
	config := convertQualityRuleConfigFromV1(request.GetConfig())
	if config == nil {
		return nil, connectErrorInvalid("config is required")
	}
	dataquality.ClearRuleConfigTargetScope(config)
	if err := dataquality.ValidateRuleTemplateConfig(ruleType, config); err != nil {
		return nil, err
	}
	return &store.QualityRuleTemplateMessage{
		Title:       title,
		Description: description,
		Dimension:   dimension,
		RuleType:    ruleType,
		Config:      config,
		Severity:    severity,
	}, nil
}

// applyQualityRuleTemplateUpdate merges an update request into the current
// template. Only the masked fields change; an empty mask replaces every mutable
// field (AIP-134). The rule type is not editable: the config is always validated
// against the template's stored type.
func applyQualityRuleTemplateUpdate(current *store.QualityRuleTemplateMessage, request *v1pb.QualityRuleTemplate, maskPaths []string) (*store.QualityRuleTemplateMessage, error) {
	if current == nil {
		return nil, connectErrorInvalid("the quality rule template is required")
	}
	if request == nil {
		return nil, connectErrorInvalid("the quality rule template is required")
	}
	paths := maskPaths
	if len(paths) == 0 {
		paths = []string{"title", "description", "severity", "dimension", "config"}
	}
	updated := *current
	for _, path := range paths {
		path = strings.TrimSpace(path)
		switch path {
		case "title":
			title, err := qualityTemplateTitle(request.GetTitle())
			if err != nil {
				return nil, err
			}
			updated.Title = title
		case "description":
			description, err := qualityTemplateDescription(request.GetDescription())
			if err != nil {
				return nil, err
			}
			updated.Description = description
		case "severity":
			severity := storepb.QualitySeverity(request.GetSeverity())
			if severity == storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED {
				return nil, connectErrorInvalid("severity is required")
			}
			updated.Severity = severity
		case "dimension":
			dimension := storepb.QualityDimension(request.GetDimension())
			if dimension == storepb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED {
				return nil, connectErrorInvalid("dimension is required")
			}
			updated.Dimension = dimension
		case "config":
			config := convertQualityRuleConfigFromV1(request.GetConfig())
			if config == nil {
				return nil, connectErrorInvalid("config is required")
			}
			dataquality.ClearRuleConfigTargetScope(config)
			if err := dataquality.ValidateRuleTemplateConfig(current.RuleType, config); err != nil {
				return nil, err
			}
			updated.Config = config
		default:
			return nil, connectErrorInvalid("unsupported update path %q", path)
		}
	}
	return &updated, nil
}

// ListQualityRuleTemplates lists the workspace's custom templates, newest first.
func (s *DataQualityService) ListQualityRuleTemplates(ctx context.Context, req *connect.Request[v1pb.ListQualityRuleTemplatesRequest]) (*connect.Response[v1pb.ListQualityRuleTemplatesResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      qualityListMaximum,
		defaultLimit: qualityDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1
	templates, err := s.store.ListQualityRuleTemplates(ctx, &store.FindQualityRuleTemplateMessage{
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality rule templates")
	}
	templates, nextPageToken, err := paginate(templates, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityRuleTemplatesResponse{NextPageToken: nextPageToken}
	for _, template := range templates {
		response.QualityRuleTemplates = append(response.QualityRuleTemplates, convertQualityRuleTemplateToV1(workspaceID, template))
	}
	return connect.NewResponse(response), nil
}

// GetQualityRuleTemplate reads one custom template.
func (s *DataQualityService) GetQualityRuleTemplate(ctx context.Context, req *connect.Request[v1pb.GetQualityRuleTemplateRequest]) (*connect.Response[v1pb.QualityRuleTemplate], error) {
	workspaceID, resourceID, err := s.templateResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	template, err := s.store.GetQualityRuleTemplate(ctx, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality rule template")
	}
	if template == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality rule template %q not found", resourceID))
	}
	return connect.NewResponse(convertQualityRuleTemplateToV1(workspaceID, template)), nil
}

// CreateQualityRuleTemplate validates and stores a custom template.
func (s *DataQualityService) CreateQualityRuleTemplate(ctx context.Context, req *connect.Request[v1pb.CreateQualityRuleTemplateRequest]) (*connect.Response[v1pb.QualityRuleTemplate], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	message, err := qualityTemplateFromCreate(req.Msg.GetQualityRuleTemplate())
	if err != nil {
		return nil, err
	}
	// A template may reference a workspace code table; resolve and canonicalize it
	// like a saved rule, so deleting that table can find this reference too.
	if err := s.resolveQualityCodeTableReference(ctx, workspaceID, message.Config); err != nil {
		return nil, err
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the quality rule template id")
	}
	callerID := principalIDPointer(currentUserID(ctx))
	message.ResourceID = resourceID
	message.CreatedBy = callerID
	message.UpdatedBy = callerID
	created, err := s.store.CreateQualityRuleTemplate(ctx, message)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality rule template")
	}
	return connect.NewResponse(convertQualityRuleTemplateToV1(workspaceID, created)), nil
}

// UpdateQualityRuleTemplate replaces the masked fields of one custom template.
func (s *DataQualityService) UpdateQualityRuleTemplate(ctx context.Context, req *connect.Request[v1pb.UpdateQualityRuleTemplateRequest]) (*connect.Response[v1pb.QualityRuleTemplate], error) {
	request := req.Msg.GetQualityRuleTemplate()
	if request == nil {
		return nil, connectErrorInvalid("the quality rule template is required")
	}
	workspaceID, resourceID, err := s.templateResourceID(ctx, request.GetName())
	if err != nil {
		return nil, err
	}
	current, err := s.store.GetQualityRuleTemplate(ctx, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality rule template")
	}
	if current == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality rule template %q not found", resourceID))
	}
	updated, err := applyQualityRuleTemplateUpdate(current, request, req.Msg.GetUpdateMask().GetPaths())
	if err != nil {
		return nil, err
	}
	if err := s.resolveQualityCodeTableReference(ctx, workspaceID, updated.Config); err != nil {
		return nil, err
	}
	updated.UpdatedBy = principalIDPointer(currentUserID(ctx))
	stored, err := s.store.UpdateQualityRuleTemplate(ctx, updated)
	if err != nil {
		return nil, errors.Wrap(err, "failed to update the quality rule template")
	}
	return connect.NewResponse(convertQualityRuleTemplateToV1(workspaceID, stored)), nil
}

// DeleteQualityRuleTemplate hard-deletes one custom template. Checks already
// created from it are unaffected: their config was copied when they were saved.
func (s *DataQualityService) DeleteQualityRuleTemplate(ctx context.Context, req *connect.Request[v1pb.DeleteQualityRuleTemplateRequest]) (*connect.Response[emptypb.Empty], error) {
	_, resourceID, err := s.templateResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	template, err := s.store.GetQualityRuleTemplate(ctx, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality rule template")
	}
	if template == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality rule template %q not found", resourceID))
	}
	if err := s.store.DeleteQualityRuleTemplate(ctx, resourceID); err != nil {
		return nil, errors.Wrap(err, "failed to delete the quality rule template")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}
