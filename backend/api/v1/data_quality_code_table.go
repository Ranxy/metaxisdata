package v1

import (
	"context"
	"strings"

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
	// qualityCodeTablePrefix is the resource-name segment of one code table.
	qualityCodeTablePrefix = "qualityCodeTables/"
	// qualityCodeTableTitleMaximum and qualityCodeTableDescriptionMaximum bound the
	// display text, matching the rule title and description bounds.
	qualityCodeTableTitleMaximum       = 200
	qualityCodeTableDescriptionMaximum = 2000
)

// qualityCodeTableName renders one code table's resource name.
func qualityCodeTableName(workspaceID, resourceID string) string {
	return common.FormatWorkspace(workspaceID) + "/" + qualityCodeTablePrefix + resourceID
}

// parseQualityCodeTableName splits a code-table resource name into its workspace
// and resource id.
func parseQualityCodeTableName(name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityCodeTablePrefix)
	if err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

// qualityCodeTableResourceID parses a code-table reference and proves its
// workspace. "workspaces/-" is accepted like every other resource name.
func (s *DataQualityService) qualityCodeTableResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualityCodeTableName(name)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	if workspace == "-" {
		// "workspaces/-" is the parent-style shorthand for the current workspace. A
		// code table reference may use it, so canonicalize before proving it.
		current, err := s.workspaceID(ctx, "workspaces/-")
		if err != nil {
			return "", "", err
		}
		workspace = current
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
}

// resolveQualityCodeTableReference canonicalizes a rule or template's standard-code
// code table reference to the current workspace and proves the table exists. The
// stored config always carries the canonical name, so a later delete can find every
// referrer.
func (s *DataQualityService) resolveQualityCodeTableReference(ctx context.Context, workspaceID string, config *storepb.QualityRuleConfig) error {
	standard := config.GetStandardCode()
	if standard == nil {
		return nil
	}
	reference := strings.TrimSpace(standard.GetCodeTable())
	if reference == "" {
		if standard.GetStandard() == storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE {
			return connectErrorInvalid("a custom code table standard requires a code_table")
		}
		return nil
	}
	_, resourceID, err := s.qualityCodeTableResourceID(ctx, reference)
	if err != nil {
		return err
	}
	table, err := s.store.GetQualityCodeTable(ctx, &store.FindQualityCodeTableMessage{ResourceID: &resourceID, SummaryOnly: true})
	if err != nil {
		return errors.Wrap(err, "failed to read the code table a rule references")
	}
	if table == nil {
		return connectErrorInvalid("the code table %q does not exist", reference)
	}
	if table.ValueCount == 0 {
		return connectErrorInvalid("the code table %q has no values; add codes before referencing it", reference)
	}
	standard.CodeTable = qualityCodeTableName(workspaceID, resourceID)
	return nil
}

// convertQualityCodeTableToV1 maps one code table. A list response omits the values
// (only the count is needed to pick a table); a get response carries them.
func convertQualityCodeTableToV1(workspaceID string, table *store.QualityCodeTableMessage, includeValues bool) *v1pb.QualityCodeTable {
	if table == nil {
		return nil
	}
	converted := &v1pb.QualityCodeTable{
		Name:        qualityCodeTableName(workspaceID, table.ResourceID),
		Title:       table.Title,
		Description: table.Description,
		ValueCount:  int32(table.ValueCount),
		CreatedBy:   qualityUserResource(table.CreatedBy),
		UpdatedBy:   qualityUserResource(table.UpdatedBy),
	}
	if includeValues {
		converted.Values = append([]string(nil), table.Values...)
	}
	if !table.CreatedAt.IsZero() {
		converted.CreateTime = timestamppb.New(table.CreatedAt)
	}
	if !table.UpdatedAt.IsZero() {
		converted.UpdateTime = timestamppb.New(table.UpdatedAt)
	}
	return converted
}

// qualityUserResource renders a principal id as a user resource name, or "" when
// unknown.
func qualityUserResource(id *int32) string {
	if id == nil || *id <= 0 {
		return ""
	}
	return common.FormatUserUID(int(*id))
}

// ListQualityCodeTables lists the workspace's code tables, newest first.
func (s *DataQualityService) ListQualityCodeTables(ctx context.Context, req *connect.Request[v1pb.ListQualityCodeTablesRequest]) (*connect.Response[v1pb.ListQualityCodeTablesResponse], error) {
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
	tables, err := s.store.ListQualityCodeTables(ctx, &store.FindQualityCodeTableMessage{
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the code tables")
	}
	tables, nextPageToken, err := paginate(tables, offset)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityCodeTablesResponse{NextPageToken: nextPageToken}
	for _, table := range tables {
		response.QualityCodeTables = append(response.QualityCodeTables, convertQualityCodeTableToV1(workspaceID, table, false))
	}
	return connect.NewResponse(response), nil
}

// GetQualityCodeTable returns one code table including its values.
func (s *DataQualityService) GetQualityCodeTable(ctx context.Context, req *connect.Request[v1pb.GetQualityCodeTableRequest]) (*connect.Response[v1pb.QualityCodeTable], error) {
	workspaceID, resourceID, err := s.qualityCodeTableResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	table, err := s.store.GetQualityCodeTable(ctx, &store.FindQualityCodeTableMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the code table")
	}
	if table == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("the code table %q does not exist", req.Msg.GetName()))
	}
	return connect.NewResponse(convertQualityCodeTableToV1(workspaceID, table, true)), nil
}

// CreateQualityCodeTable creates one workspace code table.
func (s *DataQualityService) CreateQualityCodeTable(ctx context.Context, req *connect.Request[v1pb.CreateQualityCodeTableRequest]) (*connect.Response[v1pb.QualityCodeTable], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	table := req.Msg.GetQualityCodeTable()
	if table == nil {
		return nil, connectErrorInvalid("the code table is required")
	}
	title, description, values, err := qualityCodeTableInput(table)
	if err != nil {
		return nil, err
	}
	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate the code table id")
	}
	callerID := principalIDPointer(currentUserID(ctx))
	created, err := s.store.CreateQualityCodeTable(ctx, &store.QualityCodeTableMessage{
		ResourceID:  resourceID,
		Title:       title,
		Description: description,
		Values:      values,
		ValueCount:  len(values),
		CreatedBy:   callerID,
		UpdatedBy:   callerID,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the code table")
	}
	return connect.NewResponse(convertQualityCodeTableToV1(workspaceID, created, true)), nil
}

// UpdateQualityCodeTable replaces the masked fields of one code table. Replacing the
// values is the import path.
func (s *DataQualityService) UpdateQualityCodeTable(ctx context.Context, req *connect.Request[v1pb.UpdateQualityCodeTableRequest]) (*connect.Response[v1pb.QualityCodeTable], error) {
	workspaceID, resourceID, err := s.qualityCodeTableResourceID(ctx, req.Msg.GetQualityCodeTable().GetName())
	if err != nil {
		return nil, err
	}
	current, err := s.store.GetQualityCodeTable(ctx, &store.FindQualityCodeTableMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the code table")
	}
	if current == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("the code table %q does not exist", req.Msg.GetQualityCodeTable().GetName()))
	}
	patch := &store.UpdateQualityCodeTableMessage{UpdatedBy: principalIDPointer(currentUserID(ctx))}
	for _, field := range req.Msg.GetUpdateMask().GetPaths() {
		switch field {
		case "title":
			title, err := qualityCodeTableTitle(req.Msg.GetQualityCodeTable().GetTitle())
			if err != nil {
				return nil, err
			}
			patch.Title = &title
		case "description":
			description, err := qualityCodeTableDescription(req.Msg.GetQualityCodeTable().GetDescription())
			if err != nil {
				return nil, err
			}
			patch.Description = &description
		case "values":
			values, err := dataquality.NormalizeCodeTableValues(req.Msg.GetQualityCodeTable().GetValues())
			if err != nil {
				return nil, connectErrorInvalid("%v", err)
			}
			patch.Values = &values
		case "":
		default:
			return nil, connectErrorInvalid("unknown update mask field %q", field)
		}
	}
	if len(req.Msg.GetUpdateMask().GetPaths()) == 0 {
		return nil, connectErrorInvalid("an update mask is required")
	}
	updated, err := s.store.UpdateQualityCodeTable(ctx, resourceID, patch)
	if err != nil {
		return nil, errors.Wrap(err, "failed to update the code table")
	}
	return connect.NewResponse(convertQualityCodeTableToV1(workspaceID, updated, true)), nil
}

// DeleteQualityCodeTable removes one code table that no rule or template references.
func (s *DataQualityService) DeleteQualityCodeTable(ctx context.Context, req *connect.Request[v1pb.DeleteQualityCodeTableRequest]) (*connect.Response[emptypb.Empty], error) {
	workspaceID, resourceID, err := s.qualityCodeTableResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	table, err := s.store.GetQualityCodeTable(ctx, &store.FindQualityCodeTableMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the code table")
	}
	if table == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("the code table %q does not exist", req.Msg.GetName()))
	}
	rules, templates, err := s.store.QualityCodeTableReferences(ctx, qualityCodeTableName(workspaceID, resourceID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to check what references the code table")
	}
	if rules > 0 || templates > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
			"the code table is still referenced by %d checks and %d rule templates; clear the references first", rules, templates))
	}
	if err := s.store.DeleteQualityCodeTable(ctx, resourceID); err != nil {
		return nil, errors.Wrap(err, "failed to delete the code table")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// qualityCodeTableInput validates and normalizes a create request's display fields
// and values.
func qualityCodeTableInput(table *v1pb.QualityCodeTable) (string, string, []string, error) {
	title, err := qualityCodeTableTitle(table.GetTitle())
	if err != nil {
		return "", "", nil, err
	}
	description, err := qualityCodeTableDescription(table.GetDescription())
	if err != nil {
		return "", "", nil, err
	}
	values, err := dataquality.NormalizeCodeTableValues(table.GetValues())
	if err != nil {
		return "", "", nil, connectErrorInvalid("%v", err)
	}
	return title, description, values, nil
}

func qualityCodeTableTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", connectErrorInvalid("a code table needs a title")
	}
	if len([]rune(title)) > qualityCodeTableTitleMaximum {
		return "", connectErrorInvalid("a code table title allows at most %d characters", qualityCodeTableTitleMaximum)
	}
	return title, nil
}

func qualityCodeTableDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if len([]rune(description)) > qualityCodeTableDescriptionMaximum {
		return "", connectErrorInvalid("a code table description allows at most %d characters", qualityCodeTableDescriptionMaximum)
	}
	return description, nil
}
