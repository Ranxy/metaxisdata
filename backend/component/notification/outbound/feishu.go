package outbound

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

// feishuReceiver posts a text message to a Feishu custom bot. The bot's signing
// secret is optional: a bot can also be restricted by keyword or IP.
type feishuReceiver struct {
	endpoint      string
	signingSecret string
	sign          bool
}

// NewFeishu builds a Feishu custom-bot receiver. The signing secret may be empty
// when sign is false.
func NewFeishu(endpoint, signingSecret string, sign bool) Receiver {
	return &feishuReceiver{endpoint: endpoint, signingSecret: signingSecret, sign: sign}
}

func (f *feishuReceiver) Post(ctx context.Context, message Message) (string, error) {
	body := map[string]any{
		"msg_type": "text",
		"content":  map[string]any{"text": chatText(message)},
	}
	if f.sign && f.signingSecret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		body["timestamp"] = timestamp
		body["sign"] = feishuSign(timestamp, f.signingSecret)
	}
	answer, err := newChatClient(f.endpoint).post(ctx, body)
	if err != nil {
		return "", err
	}
	// A Feishu bot answers either with the current {"code","msg"} shape or the
	// legacy {"StatusCode","StatusMessage"} one; both encode 0 on success.
	var response struct {
		Code          int    `json:"code"`
		Message       string `json:"msg"`
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := decodeAnswer(answer, &response, "malformed Feishu answer"); err != nil {
		return "", err
	}
	if response.Code != 0 {
		return "", fmt.Errorf("the Feishu bot rejected the message: %s", response.Message)
	}
	if response.StatusCode != 0 {
		return "", fmt.Errorf("the Feishu bot rejected the message: %s", response.StatusMessage)
	}
	return string(answer), nil
}

// feishuSign computes the bot's signature:
// base64(HMAC-SHA256(key="<timestamp>\n<secret>", data="")).
func feishuSign(timestamp, secret string) string {
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	_, _ = mac.Write(nil)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
