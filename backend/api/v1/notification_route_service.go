package v1

import (
	"context"
	"net/mail"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ListNotificationRoutes returns every configured owner/team routing rule.
func (s *NotificationService) ListNotificationRoutes(ctx context.Context, req *connect.Request[v1pb.ListNotificationRoutesRequest]) (*connect.Response[v1pb.ListNotificationRoutesResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	routes, err := s.store.ListNotificationRoutes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list the notification routes"))
	}
	response := &v1pb.ListNotificationRoutesResponse{}
	for _, route := range routes {
		response.NotificationRoutes = append(response.NotificationRoutes, convertToV1NotificationRoute(route, workspaceID))
	}
	return connect.NewResponse(response), nil
}

// GetNotificationRoute returns one routing rule.
func (s *NotificationService) GetNotificationRoute(ctx context.Context, req *connect.Request[v1pb.GetNotificationRouteRequest]) (*connect.Response[v1pb.NotificationRoute], error) {
	workspaceID, err := s.workspaceID(ctx, workspaceFromName(req.Msg.GetName()))
	if err != nil {
		return nil, err
	}
	resourceID, err := common.GetNotificationRouteID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	route, err := s.store.GetNotificationRoute(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification route"))
	}
	if route == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification route %q not found", req.Msg.GetName()))
	}
	return connect.NewResponse(convertToV1NotificationRoute(route, workspaceID)), nil
}

// CreateNotificationRoute stores one routing rule.
func (s *NotificationService) CreateNotificationRoute(ctx context.Context, req *connect.Request[v1pb.CreateNotificationRouteRequest]) (*connect.Response[v1pb.NotificationRoute], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	payload, err := storeNotificationRoutePayload(req.Msg.GetRoute())
	if err != nil {
		return nil, err
	}
	if err := s.validateNotificationRouteMatch(ctx, payload.GetMatch()); err != nil {
		return nil, err
	}
	if err := s.validateNotificationRouteChannels(ctx, payload, nil); err != nil {
		return nil, err
	}
	created, err := s.store.CreateNotificationRoute(ctx, &store.CreateNotificationRouteMessage{Payload: payload})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create the notification route"))
	}
	return connect.NewResponse(convertToV1NotificationRoute(created, workspaceID)), nil
}

// UpdateNotificationRoute rewrites one routing rule.
func (s *NotificationService) UpdateNotificationRoute(ctx context.Context, req *connect.Request[v1pb.UpdateNotificationRouteRequest]) (*connect.Response[v1pb.NotificationRoute], error) {
	workspaceID, err := s.workspaceID(ctx, workspaceFromName(req.Msg.GetRoute().GetName()))
	if err != nil {
		return nil, err
	}
	resourceID, err := common.GetNotificationRouteID(req.Msg.GetRoute().GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	existing, err := s.store.GetNotificationRoute(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification route"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification route %q not found", req.Msg.GetRoute().GetName()))
	}
	payload, err := mergeNotificationRouteUpdate(req.Msg.GetRoute(), req.Msg.GetUpdateMask().GetPaths(), existing.Payload)
	if err != nil {
		return nil, err
	}
	if err := s.validateNotificationRouteMatch(ctx, payload.GetMatch()); err != nil {
		return nil, err
	}
	if err := s.validateNotificationRouteChannels(ctx, payload, existing.Payload); err != nil {
		return nil, err
	}
	updated, err := s.store.UpdateNotificationRoute(ctx, &store.UpdateNotificationRouteMessage{
		ResourceID: resourceID,
		Payload:    payload,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update the notification route"))
	}
	if updated == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification route %q not found", req.Msg.GetRoute().GetName()))
	}
	return connect.NewResponse(convertToV1NotificationRoute(updated, workspaceID)), nil
}

// DeleteNotificationRoute removes one routing rule.
func (s *NotificationService) DeleteNotificationRoute(ctx context.Context, req *connect.Request[v1pb.DeleteNotificationRouteRequest]) (*connect.Response[emptypb.Empty], error) {
	if _, err := s.workspaceID(ctx, workspaceFromName(req.Msg.GetName())); err != nil {
		return nil, err
	}
	resourceID, err := common.GetNotificationRouteID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	existing, err := s.store.GetNotificationRoute(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification route"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification route %q not found", req.Msg.GetName()))
	}
	if err := s.store.DeleteNotificationRoute(ctx, resourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete the notification route"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// validateNotificationRouteChannels refuses a route that names a channel the
// workspace no longer has, so a typo surfaces at write time rather than silently
// dropping the route's events. On update only newly added channels are checked: a
// channel deleted after the route was written must not block renaming or
// disabling the route.
func (s *NotificationService) validateNotificationRouteChannels(ctx context.Context, payload, existing *storepb.NotificationRoute) error {
	kept := make(map[string]bool, len(payload.GetChannels()))
	if existing != nil {
		for _, resourceID := range existing.GetChannels() {
			kept[resourceID] = true
		}
	}
	for _, resourceID := range payload.GetChannels() {
		if kept[resourceID] {
			continue
		}
		channel, err := s.store.GetNotificationChannel(ctx, resourceID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification channel"))
		}
		if channel == nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("notification channel %q not found", resourceID))
		}
	}
	return nil
}

// validateNotificationRouteMatch checks that the people and groups a route names
// exist and that its enums are known. A typo would otherwise make a routed_only
// channel receive nothing, silently.
func (s *NotificationService) validateNotificationRouteMatch(ctx context.Context, match *storepb.NotificationRouteMatch) error {
	if match == nil {
		return nil
	}
	for _, eventType := range match.GetEventTypes() {
		switch eventType {
		case storepb.NotificationType_NOTIFICATION_TYPE_SCHEMA_SYNC,
			storepb.NotificationType_NOTIFICATION_TYPE_OPENLINEAGE,
			storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY:
		default:
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported event type %q", eventType.String()))
		}
	}
	switch match.GetMinSeverity() {
	case storepb.NotificationSeverity_NOTIFICATION_SEVERITY_UNSPECIFIED,
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO,
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR:
	default:
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported severity %q", match.GetMinSeverity().String()))
	}
	for _, owner := range match.GetOwners() {
		userID, err := common.GetUserID(owner)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid owner %q", owner))
		}
		user, err := s.store.GetUserByID(ctx, userID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the route owner"))
		}
		if user == nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("owner %q not found", owner))
		}
	}
	for _, group := range match.GetGroups() {
		found, err := s.store.GetGroup(ctx, group)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the route group"))
		}
		if found == nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("group %q not found", group))
		}
	}
	return nil
}

// storeNotificationRoutePayload converts a create request into the stored shape.
func storeNotificationRoutePayload(route *v1pb.NotificationRoute) (*storepb.NotificationRoute, error) {
	if route == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a notification route is required"))
	}
	match, err := storeNotificationRouteMatch(route.GetMatch())
	if err != nil {
		return nil, err
	}
	channels, err := storeNotificationRouteChannels(route.GetChannels())
	if err != nil {
		return nil, err
	}
	payload := &storepb.NotificationRoute{
		DisplayName: strings.TrimSpace(route.GetDisplayName()),
		Enabled:     route.GetEnabled(),
		Match:       match,
		Channels:    channels,
	}
	if err := validateNotificationRoutePayload(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// mergeNotificationRouteUpdate applies the update mask to the stored route. An
// empty mask is a full update of the mutable fields.
func mergeNotificationRouteUpdate(update *v1pb.NotificationRoute, paths []string, existing *storepb.NotificationRoute) (*storepb.NotificationRoute, error) {
	if update == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a notification route is required"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the notification route no longer exists"))
	}
	if len(paths) == 0 {
		paths = []string{"display_name", "enabled", "match", "channels"}
	}
	merged, ok := proto.Clone(existing).(*storepb.NotificationRoute)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to clone the notification route"))
	}
	for _, path := range paths {
		switch path {
		case "display_name", "displayName":
			merged.DisplayName = strings.TrimSpace(update.GetDisplayName())
		case "enabled":
			merged.Enabled = update.GetEnabled()
		case "match":
			match, err := storeNotificationRouteMatch(update.GetMatch())
			if err != nil {
				return nil, err
			}
			merged.Match = match
		case "channels":
			channels, err := storeNotificationRouteChannels(update.GetChannels())
			if err != nil {
				return nil, err
			}
			merged.Channels = channels
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update path %q", path))
		}
	}
	if err := validateNotificationRoutePayload(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// storeNotificationRouteMatch converts the request's match. Owners are stored as
// users/{id} and groups as lower-case addresses, both de-duplicated.
func storeNotificationRouteMatch(match *v1pb.NotificationRouteMatch) (*storepb.NotificationRouteMatch, error) {
	result := &storepb.NotificationRouteMatch{
		MinSeverity: storepb.NotificationSeverity(match.GetMinSeverity()),
	}
	for _, eventType := range match.GetEventTypes() {
		kind := storepb.NotificationType(eventType)
		if kind == storepb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED {
			continue
		}
		if !slices.Contains(result.EventTypes, kind) {
			result.EventTypes = append(result.EventTypes, kind)
		}
	}
	for _, owner := range match.GetOwners() {
		userID, err := common.GetUserID(strings.TrimSpace(owner))
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid owner %q", owner))
		}
		name := common.FormatUserUID(userID)
		if !slices.Contains(result.Owners, name) {
			result.Owners = append(result.Owners, name)
		}
	}
	for _, group := range match.GetGroups() {
		address := strings.TrimSpace(group)
		if address == "" {
			continue
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || strings.ContainsAny(address, "\r\n,;") {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid group %q", group))
		}
		key := strings.ToLower(address)
		if !slices.Contains(result.Groups, key) {
			result.Groups = append(result.Groups, key)
		}
	}
	return result, nil
}

// storeNotificationRouteChannels converts channel resource names to the resource
// ids the payload stores.
func storeNotificationRouteChannels(channels []string) ([]string, error) {
	result := make([]string, 0, len(channels))
	for _, channel := range channels {
		resourceID, err := common.GetNotificationChannelID(strings.TrimSpace(channel))
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if !slices.Contains(result, resourceID) {
			result = append(result, resourceID)
		}
	}
	return result, nil
}

// validateNotificationRoutePayload checks the fields a route must carry
// regardless of how it was written.
func validateNotificationRoutePayload(payload *storepb.NotificationRoute) error {
	if payload == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("a notification route is required"))
	}
	if payload.GetDisplayName() == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("a route display name is required"))
	}
	if len(payload.GetDisplayName()) > 100 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("the route display name is longer than 100 characters"))
	}
	return nil
}

// convertToV1NotificationRoute renders one stored route.
func convertToV1NotificationRoute(route *store.NotificationRouteMessage, workspaceID string) *v1pb.NotificationRoute {
	if route == nil || route.Payload == nil {
		return nil
	}
	converted := &v1pb.NotificationRoute{
		Name:        common.FormatWorkspace(workspaceID) + "/notificationRoutes/" + route.ResourceID,
		DisplayName: route.Payload.GetDisplayName(),
		Enabled:     route.Payload.GetEnabled(),
		CreateTime:  timestamppb.New(route.CreatedAt),
		UpdateTime:  timestamppb.New(route.UpdatedAt),
	}
	if match := route.Payload.GetMatch(); match != nil {
		converted.Match = &v1pb.NotificationRouteMatch{
			MinSeverity: v1pb.NotificationSeverity(match.GetMinSeverity()),
		}
		for _, eventType := range match.GetEventTypes() {
			converted.Match.EventTypes = append(converted.Match.EventTypes, v1pb.NotificationType(eventType))
		}
		for _, owner := range match.GetOwners() {
			userID, err := common.GetUserID(owner)
			if err != nil {
				continue
			}
			converted.Match.Owners = append(converted.Match.Owners, common.FormatUserUID(userID))
		}
		converted.Match.Groups = append(converted.Match.Groups, match.GetGroups()...)
	}
	for _, resourceID := range route.Payload.GetChannels() {
		converted.Channels = append(converted.Channels, common.FormatWorkspace(workspaceID)+"/notificationChannels/"+resourceID)
	}
	return converted
}
