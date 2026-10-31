package notification

import (
	"context"
	"log/slog"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// routedChannelIDs returns the channels a routing rule selected for one event.
// Routing is additive: a matching rule selects its channels on top of the ones
// their subscription admits, and a channel marked routed_only is dropped unless a
// rule selected it. A route with no filters matches every event, so the common
// case is a team rule narrowing one channel to its own owners.
//
// Routing is best effort: an unreadable rule table or group list logs and falls
// back to the subscription fan-out rather than dropping an event.
func (s *Service) routedChannelIDs(ctx context.Context, event *storepb.NotificationDeliveryEvent, channels []*store.NotificationChannelMessage) map[int64]bool {
	routes, err := s.store.ListNotificationRoutes(ctx)
	if err != nil {
		slog.Warn("Failed to read the notification routing rules", clog.WithError(err))
		return nil
	}
	if len(routes) == 0 {
		return nil
	}
	byResourceID := make(map[string]*store.NotificationChannelMessage, len(channels))
	for _, channel := range channels {
		byResourceID[channel.ResourceID] = channel
	}
	audience := make(map[int32]bool, len(event.GetAudienceUserIds()))
	for _, userID := range event.GetAudienceUserIds() {
		audience[userID] = true
	}
	groups := s.routingGroups(ctx, routes)
	selected := make(map[int64]bool)
	for _, route := range routes {
		if !routeMatches(route, event, audience, groups) {
			continue
		}
		for _, resourceID := range route.Payload.GetChannels() {
			channel := byResourceID[resourceID]
			if channel == nil || !subscribes(channel.Payload, event) {
				continue
			}
			selected[channel.ID] = true
		}
	}
	return selected
}

// routingGroups resolves the group filters every route names to the user ids
// that belong to those groups. It only reads groups when a route actually names
// one.
func (s *Service) routingGroups(ctx context.Context, routes []*store.NotificationRouteMessage) map[string][]int32 {
	needed := make(map[string]bool)
	for _, route := range routes {
		if !route.Payload.GetEnabled() {
			continue
		}
		for _, group := range route.Payload.GetMatch().GetGroups() {
			needed[strings.ToLower(group)] = true
		}
	}
	if len(needed) == 0 {
		return nil
	}
	groups, err := s.store.ListGroups(ctx, &store.FindGroupMessage{})
	if err != nil {
		slog.Warn("Failed to read the groups named by notification routing rules", clog.WithError(err))
		return nil
	}
	members := make(map[string][]int32, len(groups))
	for _, group := range groups {
		key := strings.ToLower(group.Email)
		if !needed[key] || group.Payload == nil {
			continue
		}
		for _, member := range group.Payload.GetMembers() {
			userID, err := common.GetUserID(member.GetMember())
			if err != nil {
				continue
			}
			members[key] = append(members[key], int32(userID))
		}
	}
	return members
}

// routeMatches reports whether one route selects one event: its type filter, its
// severity floor and the audience filters all have to accept it.
func routeMatches(route *store.NotificationRouteMessage, event *storepb.NotificationDeliveryEvent, audience map[int32]bool, groups map[string][]int32) bool {
	if route == nil || !route.Payload.GetEnabled() {
		return false
	}
	match := route.Payload.GetMatch()
	if types := match.GetEventTypes(); len(types) > 0 {
		found := false
		for _, eventType := range types {
			if eventType == event.GetType() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if severityRank(event.GetSeverity()) < severityRank(match.GetMinSeverity()) {
		return false
	}
	owners := match.GetOwners()
	groupFilters := match.GetGroups()
	if len(owners) == 0 && len(groupFilters) == 0 {
		return true
	}
	for _, owner := range owners {
		userID, err := common.GetUserID(owner)
		if err != nil {
			continue
		}
		if audience[int32(userID)] {
			return true
		}
	}
	for _, group := range groupFilters {
		for _, userID := range groups[strings.ToLower(group)] {
			if audience[userID] {
				return true
			}
		}
	}
	return false
}
