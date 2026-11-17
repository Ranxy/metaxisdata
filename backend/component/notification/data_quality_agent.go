package notification

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualityAgentAttentionMessage builds the in-app envelope of one agent escalation.
// The client renders the prose from the detail and its i18n catalog.
func QualityAgentAttentionMessage(severity storepb.NotificationSeverity, detail *storepb.QualityAgentAttentionDetail) *storepb.Notification {
	return &storepb.Notification{
		Type:     storepb.NotificationType_NOTIFICATION_TYPE_DATA_QUALITY_AGENT,
		Severity: severity,
		Detail:   &storepb.Notification_QualityAgentAttention{QualityAgentAttention: boundQualityAgentAttentionDetail(detail)},
	}
}

// SendQualityAgentAttention writes the owner's inbox row. It is called from the
// session runner when a session needs a person; the attention row is the durable
// to-do, this notification is only the doorbell.
func (s *Service) SendQualityAgentAttention(ctx context.Context, ownerID int32, dedupeKey string, detail *storepb.QualityAgentAttentionDetail) error {
	if ownerID <= 0 {
		return errors.New("a quality agent owner is required")
	}
	if detail == nil {
		return errors.New("a quality agent attention detail is required")
	}
	message := QualityAgentAttentionMessage(storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING, detail)
	message.RecipientId = ownerID
	message.DedupeKey = dedupeKey
	return s.Send(ctx, message)
}

func boundQualityAgentAttentionDetail(detail *storepb.QualityAgentAttentionDetail) *storepb.QualityAgentAttentionDetail {
	if detail == nil {
		return nil
	}
	return &storepb.QualityAgentAttentionDetail{
		Agent:     strings.TrimSpace(detail.GetAgent()),
		Attention: strings.TrimSpace(detail.GetAttention()),
		Kind:      detail.GetKind(),
		Title:     truncateBytes(detail.GetTitle()),
		Question:  truncateBytes(detail.GetQuestion()),
	}
}
