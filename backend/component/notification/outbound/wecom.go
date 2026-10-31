package outbound

import (
	"context"
	"fmt"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// maxWeComContentBytes is the robot API's markdown body limit.
const maxWeComContentBytes = 4096

// weComReceiver posts a markdown message to a WeCom group robot. The robot key
// is part of the endpoint, so there is no signing secret.
type weComReceiver struct {
	endpoint string
}

// NewWeCom builds a WeCom robot receiver.
func NewWeCom(endpoint string) Receiver {
	return &weComReceiver{endpoint: endpoint}
}

func (w *weComReceiver) Post(ctx context.Context, message Message) (string, error) {
	content := chatMarkdown(message)
	if len(content) > maxWeComContentBytes {
		content = common.TruncateUTF8Bytes(content, maxWeComContentBytes)
	}
	answer, err := newChatClient(w.endpoint).post(ctx, map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": content},
	})
	if err != nil {
		return "", err
	}
	var response struct {
		ErrorCode    int    `json:"errcode"`
		ErrorMessage string `json:"errmsg"`
	}
	if err := decodeAnswer(answer, &response, "malformed WeCom answer"); err != nil {
		return "", err
	}
	if response.ErrorCode != 0 {
		return "", fmt.Errorf("the WeCom robot rejected the message: %s", response.ErrorMessage)
	}
	return string(answer), nil
}
