package outbound

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestValidateChannelEmail(t *testing.T) {
	t.Parallel()

	channel := func(addresses []string, includeAudience bool) *storepb.NotificationChannel {
		return &storepb.NotificationChannel{
			DisplayName: "on-call",
			Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL,
			Config: &storepb.NotificationChannelConfig{
				Config: &storepb.NotificationChannelConfig_Email{Email: &storepb.EmailChannelConfig{
					Addresses:       addresses,
					IncludeAudience: includeAudience,
				}},
			},
		}
	}

	require.NoError(t, ValidateChannel(channel([]string{"oncall@example.com"}, false), nil))
	require.NoError(t, ValidateChannel(channel(nil, true), nil), "the audience alone is a recipient source")

	require.Error(t, ValidateChannel(channel(nil, false), nil), "no recipient source")
	require.Error(t, ValidateChannel(channel([]string{"not-an-address"}, false), nil), "invalid address")
	require.Error(t, ValidateChannel(channel([]string{"oncall@example.com"}, false),
		&storepb.NotificationChannelSecret{Endpoint: "https://example.com"}), "an email channel takes no credential")
}

func TestValidateChannelWebhookLikeKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		channel *storepb.NotificationChannel
		signs   bool
	}{
		{
			name: "webhook",
			channel: &storepb.NotificationChannel{
				DisplayName: "hook",
				Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WEBHOOK,
				Config:      &storepb.NotificationChannelConfig{Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{Sign: true}}},
			},
			signs: true,
		},
		{
			name: "dingtalk",
			channel: &storepb.NotificationChannel{
				DisplayName: "dingtalk",
				Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_DINGTALK,
				Config:      &storepb.NotificationChannelConfig{Config: &storepb.NotificationChannelConfig_Dingtalk{Dingtalk: &storepb.DingTalkChannelConfig{Sign: true}}},
			},
			signs: true,
		},
		{
			name: "wecom",
			channel: &storepb.NotificationChannel{
				DisplayName: "wecom",
				Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WECOM,
				Config:      &storepb.NotificationChannelConfig{Config: &storepb.NotificationChannelConfig_Wecom{Wecom: &storepb.WeComChannelConfig{}}},
			},
		},
		{
			name: "feishu",
			channel: &storepb.NotificationChannel{
				DisplayName: "feishu",
				Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_FEISHU,
				Config:      &storepb.NotificationChannelConfig{Config: &storepb.NotificationChannelConfig_Feishu{Feishu: &storepb.FeishuChannelConfig{Sign: true}}},
			},
			signs: true,
		},
	}
	for _, tc := range cases {
		require.Error(t, ValidateChannel(tc.channel, &storepb.NotificationChannelSecret{}), "%s needs an endpoint", tc.name)
		endpointOnly := &storepb.NotificationChannelSecret{Endpoint: "https://example.com/robot"}
		if tc.signs {
			require.Error(t, ValidateChannel(tc.channel, endpointOnly), "%s signing needs a secret", tc.name)
		} else {
			require.NoError(t, ValidateChannel(tc.channel, endpointOnly), "%s does not sign", tc.name)
		}
		secret := &storepb.NotificationChannelSecret{Endpoint: "https://example.com/robot", SigningSecret: "s3cret"}
		require.NoError(t, ValidateChannel(tc.channel, secret), tc.name)
	}
}

// A config of another kind has no delivery path, so it is refused at write time.
func TestValidateChannelRejectsAMismatchedConfig(t *testing.T) {
	t.Parallel()

	require.Error(t, ValidateChannel(&storepb.NotificationChannel{
		DisplayName: "dingtalk",
		Kind:        storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_DINGTALK,
		Config:      &storepb.NotificationChannelConfig{Config: &storepb.NotificationChannelConfig_Webhook{Webhook: &storepb.WebhookChannelConfig{}}},
	}, &storepb.NotificationChannelSecret{Endpoint: "https://example.com"}))
}

func TestNewReceiverRequiresTheKindsConfigAndSecret(t *testing.T) {
	t.Parallel()

	_, err := NewReceiver(&storepb.NotificationChannel{
		Kind: storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_EMAIL,
		Config: &storepb.NotificationChannelConfig{
			Config: &storepb.NotificationChannelConfig_Email{Email: &storepb.EmailChannelConfig{IncludeAudience: true}},
		},
	}, nil, nil)
	require.ErrorContains(t, err, "email delivery is not configured")

	_, err = NewReceiver(&storepb.NotificationChannel{
		Kind:   storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WECOM,
		Config: &storepb.NotificationChannelConfig{Config: &storepb.NotificationChannelConfig_Wecom{Wecom: &storepb.WeComChannelConfig{}}},
	}, nil, nil)
	require.ErrorContains(t, err, "no endpoint")

	receiver, err := NewReceiver(&storepb.NotificationChannel{
		Kind:   storepb.NotificationChannelKind_NOTIFICATION_CHANNEL_KIND_WECOM,
		Config: &storepb.NotificationChannelConfig{Config: &storepb.NotificationChannelConfig_Wecom{Wecom: &storepb.WeComChannelConfig{}}},
	}, &storepb.NotificationChannelSecret{Endpoint: "https://example.com/robot"}, nil)
	require.NoError(t, err)
	require.NotNil(t, receiver)

	// A channel with no kind cannot be delivered at all.
	_, err = NewReceiver(&storepb.NotificationChannel{}, nil, nil)
	require.Error(t, err)

	// The context is unused by construction but keeps the signature honest for
	// the delivery path.
	_ = context.Background()
}
