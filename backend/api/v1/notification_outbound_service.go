package v1

import (
	"context"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/notification/outbound"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// defaultNotificationChannelPageSize is the page size the channel list uses. A
// workspace has a handful of destinations, so this is not a paging surface in
// practice; the list is still returned in one response because the enqueue path
// reads every channel anyway.
const defaultNotificationDeliveryPageSize = 50

// maxNotificationDeliveryPageSize bounds one page of delivery diagnostics.
const maxNotificationDeliveryPageSize = 1000

// ListNotificationChannels returns every configured outbound channel.
func (s *NotificationService) ListNotificationChannels(ctx context.Context, req *connect.Request[v1pb.ListNotificationChannelsRequest]) (*connect.Response[v1pb.ListNotificationChannelsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	channels, err := s.store.ListNotificationChannels(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list the notification channels"))
	}
	response := &v1pb.ListNotificationChannelsResponse{}
	for _, channel := range channels {
		response.NotificationChannels = append(response.NotificationChannels, convertToV1NotificationChannel(channel, workspaceID))
	}
	return connect.NewResponse(response), nil
}

// GetNotificationChannel returns one configured channel. The credential is never
// part of the response.
func (s *NotificationService) GetNotificationChannel(ctx context.Context, req *connect.Request[v1pb.GetNotificationChannelRequest]) (*connect.Response[v1pb.NotificationChannel], error) {
	workspaceID, err := s.workspaceID(ctx, workspaceFromName(req.Msg.GetName()))
	if err != nil {
		return nil, err
	}
	resourceID, err := common.GetNotificationChannelID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	channel, err := s.store.GetNotificationChannel(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification channel"))
	}
	if channel == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification channel %q not found", req.Msg.GetName()))
	}
	return connect.NewResponse(convertToV1NotificationChannel(channel, workspaceID)), nil
}

// CreateNotificationChannel stores one channel and its write-only credential.
func (s *NotificationService) CreateNotificationChannel(ctx context.Context, req *connect.Request[v1pb.CreateNotificationChannelRequest]) (*connect.Response[v1pb.NotificationChannel], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	payload, err := storeNotificationChannelPayload(req.Msg.GetChannel())
	if err != nil {
		return nil, err
	}
	secret := storeNotificationChannelSecret(req.Msg.GetSecret())
	if err := outbound.ValidateChannel(payload, secret); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	created, err := s.store.CreateNotificationChannel(ctx, &store.CreateNotificationChannelMessage{Payload: payload, Secret: secret})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create the notification channel"))
	}
	return connect.NewResponse(convertToV1NotificationChannel(created, workspaceID)), nil
}

// UpdateNotificationChannel rewrites one channel. An omitted secret keeps the
// stored credential; the kind is immutable.
func (s *NotificationService) UpdateNotificationChannel(ctx context.Context, req *connect.Request[v1pb.UpdateNotificationChannelRequest]) (*connect.Response[v1pb.NotificationChannel], error) {
	workspaceID, err := s.workspaceID(ctx, workspaceFromName(req.Msg.GetChannel().GetName()))
	if err != nil {
		return nil, err
	}
	resourceID, err := common.GetNotificationChannelID(req.Msg.GetChannel().GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	existing, err := s.store.GetNotificationChannel(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification channel"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification channel %q not found", req.Msg.GetChannel().GetName()))
	}

	payload, err := storeNotificationChannelPayload(req.Msg.GetChannel())
	if err != nil {
		return nil, err
	}
	payload.Kind = existing.Payload.GetKind()
	payload = applyNotificationChannelUpdateMask(payload, req.Msg.GetUpdateMask().GetPaths(), existing.Payload)

	// The write-only secret follows the same rule as the payload: a part the
	// request leaves empty keeps what is stored, so rotating only the signing
	// secret does not require re-entering the endpoint URL.
	stored, err := s.store.GetNotificationChannelSecret(ctx, existing.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification channel credential"))
	}
	secret := storeNotificationChannelSecret(req.Msg.GetSecret())
	if secret != nil {
		if secret.GetWebhookUrl() == "" {
			secret.WebhookUrl = stored.GetWebhookUrl()
		}
		if secret.GetWebhookSigningSecret() == "" {
			secret.WebhookSigningSecret = stored.GetWebhookSigningSecret()
		}
		if secret.GetWebhookUrl() == "" && secret.GetWebhookSigningSecret() == "" {
			secret = nil
		}
	}
	effective := secret
	if effective == nil {
		// Validation needs to know whether a credential is already stored, because
		// a signing channel without a secret can never be posted.
		effective = stored
	}
	if err := outbound.ValidateChannel(payload, effective); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	updated, err := s.store.UpdateNotificationChannel(ctx, &store.UpdateNotificationChannelMessage{
		ResourceID: resourceID,
		Payload:    payload,
		Secret:     secret,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update the notification channel"))
	}
	if updated == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification channel %q not found", req.Msg.GetChannel().GetName()))
	}
	return connect.NewResponse(convertToV1NotificationChannel(updated, workspaceID)), nil
}

// DeleteNotificationChannel removes one channel and its queued deliveries.
func (s *NotificationService) DeleteNotificationChannel(ctx context.Context, req *connect.Request[v1pb.DeleteNotificationChannelRequest]) (*connect.Response[emptypb.Empty], error) {
	if _, err := s.workspaceID(ctx, workspaceFromName(req.Msg.GetName())); err != nil {
		return nil, err
	}
	resourceID, err := common.GetNotificationChannelID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.store.DeleteNotificationChannel(ctx, resourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete the notification channel"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// TestNotificationChannel posts one test message through a stored channel. It
// writes nothing to the ledger and queues no delivery, so it is safe to press
// repeatedly while configuring a destination.
func (s *NotificationService) TestNotificationChannel(ctx context.Context, req *connect.Request[v1pb.TestNotificationChannelRequest]) (*connect.Response[v1pb.TestNotificationChannelResponse], error) {
	if _, err := s.workspaceID(ctx, workspaceFromName(req.Msg.GetName())); err != nil {
		return nil, err
	}
	resourceID, err := common.GetNotificationChannelID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	channel, err := s.store.GetNotificationChannel(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification channel"))
	}
	if channel == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification channel %q not found", req.Msg.GetName()))
	}
	secret, err := s.store.GetNotificationChannelSecret(ctx, channel.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification channel credential"))
	}
	receiver, err := outbound.NewReceiver(channel.Payload, secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	message := outbound.Message{
		EventID:    "test",
		EventType:  storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity:   storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		Title:      "Metaxisdata test notification",
		Text:       "This is a test message from Settings > Notifications. No data quality check failed.",
		OccurredAt: time.Now(),
	}
	if outbound.NormalizeLanguage(channel.Payload.GetLanguage()) == "zh-CN" {
		message.Title = "Metaxisdata 测试通知"
		message.Text = "这是来自「设置 → 通知」的测试消息，没有数据质量检查失败。"
	}
	if setting, settingErr := s.store.GetWorkspaceGeneralSetting(ctx); settingErr == nil {
		if base := strings.TrimRight(strings.TrimSpace(setting.GetExternalUrl()), "/"); base != "" {
			message.Link = base + "/settings/notifications"
		}
	}

	answer, err := receiver.Post(ctx, message)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&v1pb.TestNotificationChannelResponse{Response: answer}), nil
}

// ListNotificationDeliveries returns outbound deliveries newest first.
func (s *NotificationService) ListNotificationDeliveries(ctx context.Context, req *connect.Request[v1pb.ListNotificationDeliveriesRequest]) (*connect.Response[v1pb.ListNotificationDeliveriesResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	filter := store.NotificationDeliveryFilter{State: storepb.NotificationDeliveryState(req.Msg.GetState())}
	if channel := strings.TrimSpace(req.Msg.GetChannel()); channel != "" {
		resourceID, parseErr := common.GetNotificationChannelID(channel)
		if parseErr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, parseErr)
		}
		stored, readErr := s.store.GetNotificationChannel(ctx, resourceID)
		if readErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(readErr, "failed to read the notification channel"))
		}
		if stored == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification channel %q not found", channel))
		}
		filter.ChannelID = stored.ID
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      maxNotificationDeliveryPageSize,
		defaultLimit: defaultNotificationDeliveryPageSize,
	})
	if err != nil {
		return nil, err
	}
	filter.Limit = offset.limit + 1
	filter.Offset = offset.offset

	deliveries, err := s.store.ListNotificationDeliveries(ctx, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list the notification deliveries"))
	}
	deliveries, nextPageToken, err := paginate(deliveries, offset)
	if err != nil {
		return nil, err
	}

	channels := s.notificationChannelsByID(ctx)
	response := &v1pb.ListNotificationDeliveriesResponse{NextPageToken: nextPageToken}
	for _, delivery := range deliveries {
		response.NotificationDeliveries = append(response.NotificationDeliveries,
			convertToV1NotificationDelivery(delivery, channels[delivery.ChannelID], workspaceID))
	}
	return connect.NewResponse(response), nil
}

// RetryNotificationDelivery requeues one DEAD delivery. A delivery that is still
// waiting, already sent or already retried is returned unchanged.
func (s *NotificationService) RetryNotificationDelivery(ctx context.Context, req *connect.Request[v1pb.RetryNotificationDeliveryRequest]) (*connect.Response[v1pb.NotificationDelivery], error) {
	workspaceID, err := s.workspaceID(ctx, workspaceFromName(req.Msg.GetName()))
	if err != nil {
		return nil, err
	}
	deliveryID, err := common.GetNotificationDeliveryID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	delivery, err := s.store.RequeueNotificationDelivery(ctx, deliveryID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to retry the notification delivery"))
	}
	if delivery == nil {
		existing, readErr := s.store.GetNotificationDelivery(ctx, deliveryID)
		if readErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(readErr, "failed to read the notification delivery"))
		}
		if existing == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("notification delivery %q not found", req.Msg.GetName()))
		}
		delivery = existing
	}
	channels := s.notificationChannelsByID(ctx)
	return connect.NewResponse(convertToV1NotificationDelivery(delivery, channels[delivery.ChannelID], workspaceID)), nil
}

// notificationChannelsByID resolves the channels one delivery listing needs.
// A channel deleted after its deliveries were queued leaves no entry; the listing
// still shows the delivery, because it is the record of what happened.
func (s *NotificationService) notificationChannelsByID(ctx context.Context) map[int64]*store.NotificationChannelMessage {
	byID := map[int64]*store.NotificationChannelMessage{}
	channels, err := s.store.ListNotificationChannels(ctx)
	if err != nil {
		return byID
	}
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	return byID
}

// storeNotificationChannelPayload converts the write shape into the stored one.
func storeNotificationChannelPayload(channel *v1pb.NotificationChannel) (*storepb.NotificationChannel, error) {
	if channel == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a notification channel is required"))
	}
	displayName := strings.TrimSpace(channel.GetDisplayName())
	if displayName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a channel display name is required"))
	}
	if len(displayName) > 100 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the channel display name is longer than 100 characters"))
	}
	language := strings.TrimSpace(channel.GetLanguage())
	if len(language) > 35 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the channel language is not a BCP-47 tag"))
	}

	payload := &storepb.NotificationChannel{
		DisplayName: displayName,
		Kind:        storepb.NotificationChannelKind(channel.GetKind()),
		Enabled:     channel.GetEnabled(),
		MinSeverity: storepb.NotificationSeverity(channel.GetMinSeverity()),
		Language:    language,
		Config:      &storepb.NotificationChannelConfig{},
	}
	if webhook := channel.GetConfig().GetWebhook(); webhook != nil {
		payload.Config.Config = &storepb.NotificationChannelConfig_Webhook{
			Webhook: &storepb.WebhookChannelConfig{Sign: webhook.GetSign()},
		}
	} else {
		payload.Config.Config = &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{}}
	}
	for _, eventType := range channel.GetEventTypes() {
		kind := storepb.NotificationType(eventType)
		if kind == storepb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED {
			continue
		}
		payload.EventTypes = append(payload.EventTypes, kind)
	}
	return payload, nil
}

// storeNotificationChannelSecret converts the write-only credential. Nil means
// "keep what is stored".
func storeNotificationChannelSecret(secret *v1pb.NotificationChannelSecret) *storepb.NotificationChannelSecret {
	if secret == nil || (strings.TrimSpace(secret.GetWebhookUrl()) == "" && secret.GetWebhookSigningSecret() == "") {
		return nil
	}
	return &storepb.NotificationChannelSecret{
		WebhookUrl:           strings.TrimSpace(secret.GetWebhookUrl()),
		WebhookSigningSecret: secret.GetWebhookSigningSecret(),
	}
}

// applyNotificationChannelUpdateMask merges a masked update onto the stored
// payload. An empty mask is a full update of the mutable fields.
func applyNotificationChannelUpdateMask(update *storepb.NotificationChannel, paths []string, existing *storepb.NotificationChannel) *storepb.NotificationChannel {
	if existing == nil {
		return update
	}
	if len(paths) == 0 {
		return update
	}
	merged := &storepb.NotificationChannel{
		DisplayName: existing.GetDisplayName(),
		Kind:        existing.GetKind(),
		Enabled:     existing.GetEnabled(),
		EventTypes:  existing.GetEventTypes(),
		MinSeverity: existing.GetMinSeverity(),
		Language:    existing.GetLanguage(),
		Config:      existing.GetConfig(),
	}
	for _, path := range paths {
		switch path {
		case "display_name", "displayName":
			merged.DisplayName = update.GetDisplayName()
		case "enabled":
			merged.Enabled = update.GetEnabled()
		case "event_types", "eventTypes":
			merged.EventTypes = update.GetEventTypes()
		case "min_severity", "minSeverity":
			merged.MinSeverity = update.GetMinSeverity()
		case "language":
			merged.Language = update.GetLanguage()
		case "config":
			merged.Config = update.GetConfig()
		default:
			// Unknown paths are ignored: the mask is advisory here, and the stored
			// payload stays valid for every field the request did not name.
		}
	}
	return merged
}

// workspaceFromName returns the workspaces/{workspace} prefix of a resource name.
func workspaceFromName(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) >= 2 && parts[0] == "workspaces" && parts[1] != "" {
		return parts[0] + "/" + parts[1]
	}
	return name
}

// convertToV1NotificationChannel renders one stored channel. The credential is
// never copied; a read reports only whether one is configured.
func convertToV1NotificationChannel(channel *store.NotificationChannelMessage, workspaceID string) *v1pb.NotificationChannel {
	if channel == nil || channel.Payload == nil {
		return nil
	}
	converted := &v1pb.NotificationChannel{
		Name:        common.FormatWorkspace(workspaceID) + "/notificationChannels/" + channel.ResourceID,
		DisplayName: channel.Payload.GetDisplayName(),
		Kind:        v1pb.NotificationChannelKind(channel.Payload.GetKind()),
		Enabled:     channel.Payload.GetEnabled(),
		MinSeverity: v1pb.NotificationSeverity(channel.Payload.GetMinSeverity()),
		Language:    channel.Payload.GetLanguage(),
		SecretSet:   channel.HasSecret,
	}
	for _, eventType := range channel.Payload.GetEventTypes() {
		converted.EventTypes = append(converted.EventTypes, v1pb.NotificationType(eventType))
	}
	if webhook := channel.Payload.GetConfig().GetWebhook(); webhook != nil {
		converted.Config = &v1pb.NotificationChannelConfig{
			Config: &v1pb.NotificationChannelConfig_Webhook{
				Webhook: &v1pb.WebhookChannelConfig{Sign: webhook.GetSign()},
			},
		}
	}
	converted.CreateTime = timestamppb.New(channel.CreatedAt)
	converted.UpdateTime = timestamppb.New(channel.UpdatedAt)
	return converted
}

// convertToV1NotificationDelivery renders one queued or attempted delivery.
func convertToV1NotificationDelivery(delivery *store.NotificationDeliveryMessage, channel *store.NotificationChannelMessage, workspaceID string) *v1pb.NotificationDelivery {
	if delivery == nil {
		return nil
	}
	converted := &v1pb.NotificationDelivery{
		Name:            common.FormatWorkspace(workspaceID) + "/notificationDeliveries/" + strconv.FormatInt(delivery.ID, 10),
		EventType:       v1pb.NotificationType(delivery.Payload.GetType()),
		Severity:        v1pb.NotificationSeverity(delivery.Payload.GetSeverity()),
		State:           v1pb.NotificationDeliveryState(delivery.State),
		Attempts:        delivery.Attempts,
		LastError:       delivery.LastError,
		CreateTime:      timestamppb.New(delivery.CreatedAt),
		NextAttemptTime: timestamppb.New(delivery.NextAttempt),
	}
	if delivery.SentAt != nil {
		converted.SentTime = timestamppb.New(*delivery.SentAt)
	}
	if channel != nil {
		converted.Channel = common.FormatWorkspace(workspaceID) + "/notificationChannels/" + channel.ResourceID
		converted.ChannelName = channel.Payload.GetDisplayName()
	}
	return converted
}
