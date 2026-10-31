package outbound

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestNormalizeEmailAddresses(t *testing.T) {
	t.Parallel()

	addresses, err := NormalizeEmailAddresses([]string{" OnCall@example.com ", "", "oncall@example.com", "lead@example.com"})
	require.NoError(t, err)
	require.Equal(t, []string{"OnCall@example.com", "lead@example.com"}, addresses)

	_, err = NormalizeEmailAddresses([]string{"not-an-address"})
	require.Error(t, err)
	_, err = NormalizeEmailAddresses([]string{"Name <name@example.com>"})
	require.Error(t, err)
	_, err = NormalizeEmailAddresses([]string{"a@example.com\r\nBcc: victim@example.com"})
	require.Error(t, err)

	tooMany := make([]string, 0, maxFixedEmailAddresses+1)
	for i := 0; i <= maxFixedEmailAddresses; i++ {
		tooMany = append(tooMany, "user"+strconv.Itoa(i)+"@example.com")
	}
	_, err = NormalizeEmailAddresses(tooMany)
	require.Error(t, err)
}

func TestEmailRecipientsResolvesTheAudience(t *testing.T) {
	t.Parallel()

	config := &storepb.EmailChannelConfig{
		Addresses:       []string{"oncall@example.com"},
		IncludeAudience: true,
	}
	emails := map[int32]string{7: "owner@example.com", 8: "ONCALL@example.com", 9: ""}
	recipients, err := EmailRecipients(context.Background(), config, []int32{7, 8, 9, 0}, func(_ context.Context, userID int32) (string, error) {
		return emails[userID], nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"oncall@example.com", "owner@example.com"}, recipients)
}

// The audience is only read when the channel opted in.
func TestEmailRecipientsSkipsTheAudienceWhenNotIncluded(t *testing.T) {
	t.Parallel()

	recipients, err := EmailRecipients(context.Background(), &storepb.EmailChannelConfig{
		Addresses: []string{"oncall@example.com"},
	}, []int32{7}, func(context.Context, int32) (string, error) {
		return "owner@example.com", nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"oncall@example.com"}, recipients)
}

// A store failure while resolving a person aborts the delivery so the dispatcher
// retries it instead of silently dropping that person.
func TestEmailRecipientsPropagatesResolverErrors(t *testing.T) {
	t.Parallel()

	_, err := EmailRecipients(context.Background(), &storepb.EmailChannelConfig{IncludeAudience: true}, []int32{7},
		func(context.Context, int32) (string, error) {
			return "", errors.New("store is down")
		})
	require.ErrorContains(t, err, "store is down")
}

func TestEmailRecipientsBoundsTheList(t *testing.T) {
	t.Parallel()

	audience := make([]int32, 0, maxEmailRecipients+1)
	for i := 1; i <= maxEmailRecipients+1; i++ {
		audience = append(audience, int32(i))
	}
	_, err := EmailRecipients(context.Background(), &storepb.EmailChannelConfig{IncludeAudience: true}, audience,
		func(_ context.Context, userID int32) (string, error) {
			return "user" + strings.Repeat("x", int(userID)) + "@example.com", nil
		})
	require.Error(t, err)
}

func TestBuildEmailMessage(t *testing.T) {
	t.Parallel()

	message := buildEmailMessage("alerts@example.com", "数据质量值班",
		"数据质量：no nulls 失败", "状态：PASS → FAIL\n\nhttps://app.example.com/data-quality/executions/x")
	require.Contains(t, message, "From: =?utf-8?")
	require.Contains(t, message, "<alerts@example.com>")
	// Recipients are envelope-only; no one sees the others' addresses.
	require.Contains(t, message, "To: undisclosed-recipients:;\r\n")
	require.NotContains(t, message, "oncall@example.com")
	require.Contains(t, message, "Subject: =?utf-8?")
	require.Contains(t, message, "Auto-Submitted: auto-generated")
	require.Contains(t, message, "Content-Type: text/plain; charset=utf-8")
	require.Contains(t, message, "Content-Transfer-Encoding: quoted-printable")
	require.Contains(t, message, "Date: ")

	// A long CJK subject is truncated and folded into several encoded words, so no
	// header line can approach the RFC 5322 limit.
	long := buildEmailMessage("alerts@example.com", "", strings.Repeat("数据质量检查失败", 40), "b")
	require.Contains(t, long, "\r\n ")
	for _, line := range strings.Split(long, "\r\n") {
		require.LessOrEqual(t, len(line), 998)
	}

	// A header injection attempt in the display name is flattened, not carried.
	require.NotContains(t, buildEmailMessage("alerts@example.com", "a\r\nBcc: victim@example.com", "s", "b"), "\r\nBcc:")
}

func TestNewEmailRequiresACompleteSetting(t *testing.T) {
	t.Parallel()

	valid := func() *storepb.EmailSetting {
		return &storepb.EmailSetting{
			From: "alerts@example.com",
			Smtp: &storepb.EmailSMTPConfig{
				Host:           "smtp.example.com",
				Port:           587,
				Encryption:     storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_STARTTLS,
				Authentication: storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_NONE,
			},
		}
	}
	_, err := NewEmail(valid())
	require.NoError(t, err)

	_, err = NewEmail(nil)
	require.Error(t, err)

	for name, mutate := range map[string]func(*storepb.EmailSetting){
		"no from":      func(setting *storepb.EmailSetting) { setting.From = "" },
		"invalid from": func(setting *storepb.EmailSetting) { setting.From = "nope" },
		"no host":      func(setting *storepb.EmailSetting) { setting.Smtp.Host = "" },
		"no port":      func(setting *storepb.EmailSetting) { setting.Smtp.Port = 0 },
		"no encryption": func(setting *storepb.EmailSetting) {
			setting.Smtp.Encryption = storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_UNSPECIFIED
		},
		"no auth": func(setting *storepb.EmailSetting) {
			setting.Smtp.Authentication = storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_UNSPECIFIED
		},
		"no login user": func(setting *storepb.EmailSetting) {
			setting.Smtp.Authentication = storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_PLAIN
		},
		"authentication needs encryption": func(setting *storepb.EmailSetting) {
			setting.Smtp.Encryption = storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE
			setting.Smtp.Authentication = storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_PLAIN
			setting.Smtp.Username = "alerts"
			setting.Smtp.Password = "s3cret"
		},
	} {
		setting := valid()
		mutate(setting)
		_, err := NewEmail(setting)
		require.Error(t, err, name)
	}
}

func TestEmailPostRequiresARecipient(t *testing.T) {
	t.Parallel()

	receiver, err := NewEmail(&storepb.EmailSetting{
		From: "alerts@example.com",
		Smtp: &storepb.EmailSMTPConfig{
			Host:           "smtp.example.com",
			Port:           25,
			Encryption:     storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE,
			Authentication: storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_NONE,
		},
	})
	require.NoError(t, err)
	_, err = receiver.Post(context.Background(), Message{Title: "t", Text: "b"})
	require.Error(t, err)
}
