package outbound

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// dingTalkReceiver posts a markdown card to a DingTalk group robot. The robot's
// signing secret is optional: a robot can also be restricted by keyword or IP.
type dingTalkReceiver struct {
	endpoint      string
	signingSecret string
	sign          bool
}

// NewDingTalk builds a DingTalk robot receiver. The signing secret may be empty
// when sign is false.
func NewDingTalk(endpoint, signingSecret string, sign bool) Receiver {
	return &dingTalkReceiver{endpoint: endpoint, signingSecret: signingSecret, sign: sign}
}

func (d *dingTalkReceiver) Post(ctx context.Context, message Message) (string, error) {
	endpoint := d.endpoint
	if d.sign && d.signingSecret != "" {
		endpoint = signedDingTalkEndpoint(d.endpoint, d.signingSecret, time.Now())
	}
	answer, err := newChatClient(endpoint).post(ctx, map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": message.Title,
			"text":  chatMarkdown(message),
		},
	})
	if err != nil {
		return "", err
	}
	var response struct {
		ErrorCode    int    `json:"errcode"`
		ErrorMessage string `json:"errmsg"`
	}
	if err := decodeAnswer(answer, &response, "malformed DingTalk answer"); err != nil {
		return "", err
	}
	if response.ErrorCode != 0 {
		return "", fmt.Errorf("the DingTalk robot rejected the message: %s", response.ErrorMessage)
	}
	return string(answer), nil
}

// signedDingTalkEndpoint adds the timestamp and HMAC-SHA256 signature the robot
// verifies: sign = base64(HMAC-SHA256(key=secret, "<timestamp>\n<secret>")).
func signedDingTalkEndpoint(endpoint, secret string, now time.Time) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "\n" + secret))
	query := parsed.Query()
	query.Set("timestamp", timestamp)
	query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
