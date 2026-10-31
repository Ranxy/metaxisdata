package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxChatResponseBytes bounds how much of a chat endpoint's answer is kept in
// the error or the test-send response. A robot answers with a short JSON
// document; a broken one can answer with a page.
const maxChatResponseBytes = 4 << 10

// chatClient posts one JSON body to one group-robot endpoint. Every chat kind
// speaks JSON over POST and reports a protocol-level error in the body, so the
// transport half is shared and only the payload differs.
type chatClient struct {
	endpoint string
	client   *http.Client
}

func newChatClient(endpoint string) *chatClient {
	return &chatClient{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: timeout,
			// A redirect is not followed on purpose: the endpoint URL carries the
			// robot token, and re-posting to a host the endpoint chose would hand
			// that token to it. A 3xx is therefore a failed attempt.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// post sends body and returns the bounded answer. A non-2xx status fails even
// when the body looks successful, because a proxy or an error page is not a
// delivered message.
func (c *chatClient) post(ctx context.Context, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the chat message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to build the chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "metaxisdata")
	resp, err := c.client.Do(req)
	if err != nil {
		// The transport quotes the endpoint verbatim, and a robot endpoint is the
		// credential. Replace it before the message reaches an API response, an
		// audit row or the delivery's last_error.
		return nil, fmt.Errorf("the chat endpoint could not be reached: %s", sanitizeEndpoint(c.endpoint, err.Error()))
	}
	defer resp.Body.Close()
	answer, readErr := io.ReadAll(io.LimitReader(resp.Body, maxChatResponseBytes))
	if readErr != nil {
		return nil, fmt.Errorf("failed to read the chat answer: %w", readErr)
	}
	trimmed := truncate(strings.TrimSpace(string(answer)), 1000)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the chat endpoint answered %s: %s", resp.Status, trimmed)
	}
	return []byte(trimmed), nil
}

// chatMarkdown composes the markdown body the robots render: the title, the
// facts line and the link.
func chatMarkdown(message Message) string {
	var body strings.Builder
	_, _ = body.WriteString("### " + message.Title)
	if text := strings.TrimSpace(message.Text); text != "" {
		_, _ = body.WriteString("\n\n" + text)
	}
	if link := strings.TrimSpace(message.Link); link != "" {
		_, _ = body.WriteString("\n\n" + link)
	}
	return body.String()
}

// chatText composes the plain-text body Feishu's custom bot sends.
func chatText(message Message) string {
	var body strings.Builder
	_, _ = body.WriteString(message.Title)
	if text := strings.TrimSpace(message.Text); text != "" {
		_, _ = body.WriteString("\n\n" + text)
	}
	if link := strings.TrimSpace(message.Link); link != "" {
		_, _ = body.WriteString("\n\n" + link)
	}
	return body.String()
}

// sanitizeEndpoint removes the endpoint from an error message. The transport
// quotes the parsed, normalized URL, which the raw replacement would miss.
func sanitizeEndpoint(endpoint, message string) string {
	message = strings.ReplaceAll(message, endpoint, "<endpoint>")
	if parsed, err := url.Parse(endpoint); err == nil {
		message = strings.ReplaceAll(message, parsed.String(), "<endpoint>")
	}
	return message
}

// decodeAnswer unmarshals a robot's answer into a protocol error. An answer that
// is not JSON is reported as a malformed answer rather than a success: the one
// thing a delivery must not do is claim a message was delivered when the
// destination never understood it.
func decodeAnswer(answer []byte, target any, malformed string) error {
	if err := json.Unmarshal(answer, target); err != nil {
		return errors.New(malformed)
	}
	return nil
}
