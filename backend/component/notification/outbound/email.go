package outbound

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// smtpTimeout bounds one SMTP transaction when the caller's context has no
// earlier deadline.
const smtpTimeout = 10 * time.Second

// emailReceiver sends one message through the workspace's SMTP server. The
// recipients are resolved by the caller (the dispatcher or the test-send path),
// because who receives an email is a property of the channel and the event, not
// of the transport.
type emailReceiver struct {
	setting *storepb.EmailSetting
}

// NewEmail builds the email receiver from the decrypted workspace setting.
func NewEmail(setting *storepb.EmailSetting) (Receiver, error) {
	if setting == nil {
		return nil, errors.New("email delivery is not configured")
	}
	if parsed, err := mail.ParseAddress(setting.GetFrom()); err != nil || parsed.Address != strings.TrimSpace(setting.GetFrom()) {
		return nil, errors.New("the email setting's From address is not valid")
	}
	if strings.TrimSpace(setting.GetSmtp().GetHost()) == "" {
		return nil, errors.New("the email setting has no SMTP host")
	}
	if setting.GetSmtp().GetPort() <= 0 || setting.GetSmtp().GetPort() > 65535 {
		return nil, errors.New("the email setting has no valid SMTP port")
	}
	encryption, err := smtpEncryption(setting)
	if err != nil {
		return nil, err
	}
	auth, err := smtpAuth(setting)
	if err != nil {
		return nil, err
	}
	if auth != nil && encryption == storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE {
		return nil, errors.New("SMTP authentication requires transport encryption")
	}
	return &emailReceiver{setting: setting}, nil
}

func (e *emailReceiver) Post(ctx context.Context, message Message) (string, error) {
	if len(message.Recipients) == 0 {
		return "", errors.New("the email has no recipient")
	}
	body := message.Text
	if link := strings.TrimSpace(message.Link); link != "" {
		body = strings.TrimRight(body, "\r\n") + "\r\n\r\n" + link
	}
	if err := sendEmail(ctx, e.setting, message.Recipients, message.Title, body); err != nil {
		return "", err
	}
	// SMTP has no application-level answer: a completed transaction is the
	// receipt, so the test-send surface shows success without a body.
	return "", nil
}

// smtpEncryption validates and returns the configured transport security.
func smtpEncryption(setting *storepb.EmailSetting) (storepb.EmailSMTPEncryption, error) {
	switch encryption := setting.GetSmtp().GetEncryption(); encryption {
	case storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE,
		storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_STARTTLS,
		storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_SSL_TLS:
		return encryption, nil
	case storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_UNSPECIFIED:
		return encryption, errors.New("the email setting has no SMTP encryption")
	default:
		return encryption, fmt.Errorf("unsupported SMTP encryption %q", encryption.String())
	}
}

// smtpAuth builds the login mechanism. Authentication NONE means the server
// accepts mail without a login.
func smtpAuth(setting *storepb.EmailSetting) (smtp.Auth, error) {
	smtpConfig := setting.GetSmtp()
	switch authentication := smtpConfig.GetAuthentication(); authentication {
	case storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_NONE:
		return nil, nil
	case storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_PLAIN:
		if smtpConfig.GetUsername() == "" {
			return nil, errors.New("SMTP authentication has no user name")
		}
		return smtp.PlainAuth("", smtpConfig.GetUsername(), smtpConfig.GetPassword(), smtpConfig.GetHost()), nil
	case storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_LOGIN:
		if smtpConfig.GetUsername() == "" {
			return nil, errors.New("SMTP authentication has no user name")
		}
		return &loginAuth{username: smtpConfig.GetUsername(), password: smtpConfig.GetPassword()}, nil
	case storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_CRAM_MD5:
		if smtpConfig.GetUsername() == "" {
			return nil, errors.New("SMTP authentication has no user name")
		}
		return smtp.CRAMMD5Auth(smtpConfig.GetUsername(), smtpConfig.GetPassword()), nil
	case storepb.EmailSMTPAuthentication_EMAIL_SMTP_AUTHENTICATION_UNSPECIFIED:
		return nil, errors.New("the email setting has no SMTP authentication mechanism")
	default:
		return nil, fmt.Errorf("unsupported SMTP authentication %q", authentication.String())
	}
}

// loginAuth implements the LOGIN SMTP authentication mechanism.
type loginAuth struct {
	username, password string
}

func (*loginAuth) Start(_ *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	default:
		return nil, nil
	}
}

// sendEmail runs one SMTP transaction: connect, optionally STARTTLS, log in,
// deliver the message and quit. The error never includes the password; the
// server's own text is passed through because it is what an administrator needs
// to fix the configuration.
func sendEmail(ctx context.Context, setting *storepb.EmailSetting, to []string, subject, body string) error {
	smtpConfig := setting.GetSmtp()
	host := strings.TrimSpace(smtpConfig.GetHost())
	address := net.JoinHostPort(host, strconv.Itoa(int(smtpConfig.GetPort())))
	from, err := mail.ParseAddress(setting.GetFrom())
	if err != nil {
		return errors.New("the email setting's From address is not valid")
	}
	encryption, err := smtpEncryption(setting)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(smtpTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	client, err := dialSMTP(ctx, address, host, encryption, deadline)
	if err != nil {
		return fmt.Errorf("failed to connect to the SMTP server %s: %w", address, err)
	}
	defer func() { _ = client.Close() }()

	if encryption == storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_STARTTLS {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("SMTP STARTTLS failed: %w", err)
		}
	}
	auth, err := smtpAuth(setting)
	if err != nil {
		return err
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}
	// A rejected recipient must not cancel the message for everyone else. The
	// server's answer names the failing address, so it is not echoed into
	// last_error; the delivery only fails when no recipient was accepted.
	accepted := 0
	var firstErr error
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		accepted++
	}
	if accepted == 0 {
		if firstErr != nil {
			return fmt.Errorf("the SMTP server rejected every recipient: %w", firstErr)
		}
		return errors.New("the SMTP server accepted no recipient")
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := writer.Write([]byte(buildEmailMessage(from.Address, setting.GetFromName(), subject, body))); err != nil {
		_ = writer.Close()
		return fmt.Errorf("failed to write the email body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to finish the email body: %w", err)
	}
	// The message is accepted once the DATA terminator is acknowledged, so a
	// failing QUIT must not turn the attempt into a retry and a duplicate mail.
	_ = client.Quit()
	return nil
}

// dialSMTP opens the connection the configured encryption implies with a
// deadline that bounds the whole transaction, since net/smtp has no context.
func dialSMTP(ctx context.Context, address, host string, encryption storepb.EmailSMTPEncryption, deadline time.Time) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: smtpTimeout, Deadline: deadline}
	switch encryption {
	case storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_SSL_TLS:
		conn, err := tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return nil, err
		}
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return smtp.NewClient(conn, host)
	case storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_NONE,
		storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_STARTTLS:
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return smtp.NewClient(conn, host)
	case storepb.EmailSMTPEncryption_EMAIL_SMTP_ENCRYPTION_UNSPECIFIED:
		return nil, errors.New("the email setting has no SMTP encryption")
	default:
		return nil, fmt.Errorf("unsupported SMTP encryption %q", encryption.String())
	}
}

// buildEmailMessage renders a single-part UTF-8 message. The subject is
// RFC 2047-encoded and folded, and the body is quoted-printable, so a non-ASCII
// notification survives every server on the way without an 8BITMIME extension.
// Recipients are envelope-only (Bcc semantics): the To header names no one, so a
// shared on-call mailbox does not learn the internal owners' and administrators'
// addresses.
func buildEmailMessage(from, fromName, subject, body string) string {
	var message bytes.Buffer
	fromHeader := from
	if name := sanitizeHeader(fromName); name != "" {
		fromHeader = encodeHeader(name) + " <" + from + ">"
	}
	fmt.Fprintf(&message, "From: %s\r\n", fromHeader)
	_, _ = message.WriteString("To: undisclosed-recipients:;\r\n")
	fmt.Fprintf(&message, "Subject: %s\r\n", encodeHeader(truncate(sanitizeHeader(subject), 120)))
	fmt.Fprintf(&message, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	_, _ = message.WriteString("Auto-Submitted: auto-generated\r\n")
	_, _ = message.WriteString("MIME-Version: 1.0\r\n")
	_, _ = message.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	_, _ = message.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")

	var encoded bytes.Buffer
	writer := quotedprintable.NewWriter(&encoded)
	_, _ = writer.Write([]byte(body))
	_ = writer.Close()
	_, _ = message.Write(encoded.Bytes())
	if !bytes.HasSuffix(encoded.Bytes(), []byte("\r\n")) {
		_, _ = message.WriteString("\r\n")
	}
	return message.String()
}

// encodeHeader RFC 2047-encodes a header value and folds it into encoded words of
// at most ~72 characters. A single encoded word breaks on a long CJK title: every
// UTF-8 byte becomes three characters and an encoded word may not exceed 75.
func encodeHeader(value string) string {
	if value == "" {
		return ""
	}
	var words []string
	var chunk strings.Builder
	for _, r := range value {
		if chunk.Len()+utf8.RuneLen(r) > 20 {
			words = append(words, mime.QEncoding.Encode("utf-8", chunk.String()))
			chunk.Reset()
		}
		_, _ = chunk.WriteRune(r)
	}
	if chunk.Len() > 0 {
		words = append(words, mime.QEncoding.Encode("utf-8", chunk.String()))
	}
	return strings.Join(words, "\r\n ")
}

// sanitizeHeader strips the line breaks that would let a stored value inject a
// header of its own.
func sanitizeHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}
