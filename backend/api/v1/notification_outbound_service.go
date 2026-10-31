package v1

import (
	"context"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
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

	payload, err := mergeNotificationChannelUpdate(req.Msg.GetChannel(), req.Msg.GetUpdateMask().GetPaths(), existing.Payload)
	if err != nil {
		return nil, err
	}

	// The write-only secret follows the same rule as the payload: a part the
	// request leaves empty keeps what is stored, so rotating only the signing
	// secret does not require re-entering the endpoint URL.
	stored, err := s.store.GetNotificationChannelSecret(ctx, existing.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the notification channel credential"))
	}
	secret := storeNotificationChannelSecret(req.Msg.GetSecret())
	if secret != nil {
		if secret.GetEndpoint() == "" {
			secret.Endpoint = stored.GetEndpoint()
		}
		if secret.GetSigningSecret() == "" {
			secret.SigningSecret = stored.GetSigningSecret()
		}
		if secret.GetEndpoint() == "" && secret.GetSigningSecret() == "" {
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

	var emailSetting *storepb.EmailSetting
	var recipients []string
	if channel.Payload.GetKind() == storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL {
		emailSetting, err = s.store.GetEmailSettingForDelivery(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read the email setting"))
		}
		if emailSetting == nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("email delivery is not configured"))
		}
		// A test message concerns nobody, so the caller receives it when the
		// channel includes its event audience. Fixed addresses come along either way.
		var audience []int32
		caller, ok := GetUserFromContext(ctx)
		if ok && caller != nil {
			audience = []int32{int32(caller.ID)}
		}
		recipients, err = outbound.EmailRecipients(ctx, channel.Payload.GetConfig().GetEmail(), audience, func(context.Context, int32) (string, error) {
			if !ok || caller == nil {
				return "", nil
			}
			return caller.Email, nil
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		if len(recipients) == 0 {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the email channel has no test recipient"))
		}
	}
	receiver, err := outbound.NewReceiver(channel.Payload, secret, emailSetting)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	message := outbound.Message{
		EventID:    "test",
		EventType:  storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY,
		Severity:   storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		Title:      "Metaxisdata test notification",
		Text:       "This is a test message from Settings > Notifications. No data quality check failed.",
		Recipients: recipients,
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

// storeNotificationChannelPayload converts a create request into the stored
// shape. It validates the fields it is authoritative for: the credential is
// checked separately because it is write-only and its validation needs the
// stored value on update.
func storeNotificationChannelPayload(channel *v1pb.NotificationChannel) (*storepb.NotificationChannel, error) {
	if channel == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a notification channel is required"))
	}
	payload := &storepb.NotificationChannel{
		DisplayName: strings.TrimSpace(channel.GetDisplayName()),
		Kind:        storepb.NotificationChannelKind(channel.GetKind()),
		Enabled:     channel.GetEnabled(),
		MinSeverity: storepb.NotificationSeverity(channel.GetMinSeverity()),
		Language:    strings.TrimSpace(channel.GetLanguage()),
	}
	payload.EventTypes = notificationEventTypes(channel.GetEventTypes())
	config, err := storeNotificationChannelConfig(payload.Kind, channel.GetConfig())
	if err != nil {
		return nil, err
	}
	payload.Config = config
	if err := validateNotificationChannelPayload(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// storeNotificationChannelConfig converts the request's config for one kind. The
// config case must match the channel's kind: the kind is immutable, and a config
// of another case has no delivery path.
func storeNotificationChannelConfig(kind storepb.NotificationChannelKind, config *v1pb.NotificationChannelConfig) (*storepb.NotificationChannelConfig, error) {
	switch kind {
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK:
		webhook := config.GetWebhook()
		if webhook == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the webhook channel configuration is required"))
		}
		return &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{Sign: webhook.GetSign()}},
		}, nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_DINGTALK:
		dingtalk := config.GetDingtalk()
		if dingtalk == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the DingTalk channel configuration is required"))
		}
		return &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Dingtalk{Dingtalk: &storepb.DingTalkChannelConfig{Sign: dingtalk.GetSign()}},
		}, nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WECOM:
		if config.GetWecom() == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the WeCom channel configuration is required"))
		}
		return &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Wecom{Wecom: &storepb.WeComChannelConfig{}},
		}, nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_FEISHU:
		feishu := config.GetFeishu()
		if feishu == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the Feishu channel configuration is required"))
		}
		return &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Feishu{Feishu: &storepb.FeishuChannelConfig{Sign: feishu.GetSign()}},
		}, nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL:
		email := config.GetEmail()
		if email == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the email channel configuration is required"))
		}
		addresses, err := outbound.NormalizeEmailAddresses(email.GetAddresses())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Email{Email: &storepb.EmailChannelConfig{
				Addresses:       addresses,
				IncludeAudience: email.GetIncludeAudience(),
			}},
		}, nil
	case storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_UNSPECIFIED:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a notification channel kind is required"))
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported notification channel kind %q", kind.String()))
	}
}

// mergeNotificationChannelUpdate applies the update mask to the stored payload.
// An empty mask is a full update of the mutable fields; the kind itself stays
// immutable, so a full update replaces everything the request may change and
// keeps the stored kind.
func mergeNotificationChannelUpdate(update *v1pb.NotificationChannel, paths []string, existing *storepb.NotificationChannel) (*storepb.NotificationChannel, error) {
	if update == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a notification channel is required"))
	}
	if existing == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the notification channel no longer exists"))
	}
	// The kind is immutable, so a full update that names another kind is a
	// client mistake rather than a silent no-op.
	if kind := storepb.NotificationChannelKind(update.GetKind()); kind != storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_UNSPECIFIED && kind != existing.GetKind() {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the notification channel kind is immutable"))
	}
	if len(paths) == 0 {
		paths = []string{"display_name", "enabled", "event_types", "min_severity", "language", "config"}
	}
	merged, ok := proto.Clone(existing).(*storepb.NotificationChannel)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to clone the notification channel"))
	}
	for _, path := range paths {
		switch path {
		case "display_name", "displayName":
			merged.DisplayName = strings.TrimSpace(update.GetDisplayName())
		case "enabled":
			merged.Enabled = update.GetEnabled()
		case "event_types", "eventTypes":
			merged.EventTypes = notificationEventTypes(update.GetEventTypes())
		case "min_severity", "minSeverity":
			merged.MinSeverity = storepb.NotificationSeverity(update.GetMinSeverity())
		case "language":
			merged.Language = strings.TrimSpace(update.GetLanguage())
		case "config":
			config, err := storeNotificationChannelConfig(merged.Kind, update.GetConfig())
			if err != nil {
				return nil, err
			}
			merged.Config = config
		case "kind":
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the notification channel kind is immutable"))
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update path %q", path))
		}
	}
	if err := validateNotificationChannelPayload(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// validateNotificationChannelPayload checks the fields a channel must carry
// regardless of how it was written.
func validateNotificationChannelPayload(payload *storepb.NotificationChannel) error {
	if payload == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("a notification channel is required"))
	}
	if payload.GetDisplayName() == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("a channel display name is required"))
	}
	if len(payload.GetDisplayName()) > 100 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("the channel display name is longer than 100 characters"))
	}
	if len(payload.GetLanguage()) > 35 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("the channel language is not a BCP-47 tag"))
	}
	return nil
}

// notificationEventTypes converts the request's notification types, dropping the
// unspecified zero value: a filter list holding it would mean "only events of no
// type" instead of "no filter".
func notificationEventTypes(types []v1pb.NotificationType) []storepb.NotificationType {
	var converted []storepb.NotificationType
	for _, eventType := range types {
		kind := storepb.NotificationType(eventType)
		if kind == storepb.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED {
			continue
		}
		converted = append(converted, kind)
	}
	return converted
}

// storeNotificationChannelSecret converts the write-only credential. Nil means
// "keep what is stored".
func storeNotificationChannelSecret(secret *v1pb.NotificationChannelSecret) *storepb.NotificationChannelSecret {
	if secret == nil || (strings.TrimSpace(secret.GetEndpoint()) == "" && secret.GetSigningSecret() == "") {
		return nil
	}
	return &storepb.NotificationChannelSecret{
		Endpoint:      strings.TrimSpace(secret.GetEndpoint()),
		SigningSecret: secret.GetSigningSecret(),
	}
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
	converted.Config = convertToV1NotificationChannelConfig(channel.Payload.GetConfig())
	converted.CreateTime = timestamppb.New(channel.CreatedAt)
	converted.UpdateTime = timestamppb.New(channel.UpdatedAt)
	return converted
}

// convertToV1NotificationChannelConfig renders the oneof case matching the stored
// channel's kind. An unknown case stays unset rather than sending a config the
// kind cannot use.
func convertToV1NotificationChannelConfig(config *storepb.NotificationChannelConfig) *v1pb.NotificationChannelConfig {
	switch typed := config.GetConfig().(type) {
	case *storepb.NotificationChannelConfig_Webhook:
		return &v1pb.NotificationChannelConfig{
			Config: &v1pb.NotificationChannelConfig_Webhook{
				Webhook: &v1pb.WebhookChannelConfig{Sign: typed.Webhook.GetSign()},
			},
		}
	case *storepb.NotificationChannelConfig_Dingtalk:
		return &v1pb.NotificationChannelConfig{
			Config: &v1pb.NotificationChannelConfig_Dingtalk{
				Dingtalk: &v1pb.DingTalkChannelConfig{Sign: typed.Dingtalk.GetSign()},
			},
		}
	case *storepb.NotificationChannelConfig_Wecom:
		return &v1pb.NotificationChannelConfig{
			Config: &v1pb.NotificationChannelConfig_Wecom{Wecom: &v1pb.WeComChannelConfig{}},
		}
	case *storepb.NotificationChannelConfig_Feishu:
		return &v1pb.NotificationChannelConfig{
			Config: &v1pb.NotificationChannelConfig_Feishu{
				Feishu: &v1pb.FeishuChannelConfig{Sign: typed.Feishu.GetSign()},
			},
		}
	case *storepb.NotificationChannelConfig_Email:
		return &v1pb.NotificationChannelConfig{
			Config: &v1pb.NotificationChannelConfig_Email{
				Email: &v1pb.EmailChannelConfig{
					Addresses:       typed.Email.GetAddresses(),
					IncludeAudience: typed.Email.GetIncludeAudience(),
				},
			},
		}
	default:
		return nil
	}
}

// convertToV1NotificationDelivery renders one queued or attempted delivery.
func convertToV1NotificationDelivery(delivery *store.NotificationDeliveryMessage, channel *store.NotificationChannelMessage, workspaceID string) *v1pb.NotificationDelivery {
	if delivery == nil {
		return nil
	}
	converted := &v1pb.NotificationDelivery{
		Name:       common.FormatWorkspace(workspaceID) + "/notificationDeliveries/" + strconv.FormatInt(delivery.ID, 10),
		EventType:  v1pb.NotificationType(delivery.Payload.GetType()),
		Severity:   v1pb.NotificationSeverity(delivery.Payload.GetSeverity()),
		State:      v1pb.NotificationDeliveryState(delivery.State),
		Attempts:   delivery.Attempts,
		LastError:  delivery.LastError,
		CreateTime: timestamppb.New(delivery.CreatedAt),
	}
	if delivery.State == storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_PENDING ||
		delivery.State == storepb.NotificationDeliveryState_NOTIFICATION_DELIVERY_STATE_SENDING {
		converted.NextAttemptTime = timestamppb.New(delivery.NextAttempt)
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
