package outbound

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func testChatMessage() Message {
	return Message{
		EventID:    "evt-1",
		DeliveryID: 9,
		Title:      "Data quality: no nulls failed",
		Text:       "Status: PASS → FAIL · Severity: ERROR",
		Link:       "https://app.example.com/data-quality/executions/x",
	}
}

// A robot answers 200 with an errcode; only errcode 0 is a delivery.
func robotServer(t *testing.T, answer string) (*httptest.Server, *[]byte) {
	t.Helper()
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(server.Close)
	return server, &body
}

func TestDingTalkPostsMarkdownCard(t *testing.T) {
	t.Parallel()

	server, raw := robotServer(t, `{"errcode":0,"errmsg":"ok"}`)
	answer, err := NewDingTalk(server.URL, "", false).Post(context.Background(), testChatMessage())
	require.NoError(t, err)
	require.Contains(t, answer, `"errcode":0`)

	var body map[string]any
	require.NoError(t, json.Unmarshal(*raw, &body))
	require.Equal(t, "markdown", body["msgtype"])
	markdown, ok := body["markdown"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Data quality: no nulls failed", markdown["title"])
	require.Contains(t, markdown["text"], "https://app.example.com/data-quality/executions/x")
}

// The robot signature is timestamp + HMAC-SHA256 over the timestamp and secret,
// carried as query parameters.
func TestDingTalkSignsTheRequest(t *testing.T) {
	t.Parallel()

	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	_, err := NewDingTalk(server.URL, "s3cret", true).Post(context.Background(), testChatMessage())
	require.NoError(t, err)
	timestamp := query.Get("timestamp")
	require.NotEmpty(t, timestamp)
	mac := hmac.New(sha256.New, []byte("s3cret"))
	_, _ = mac.Write([]byte(timestamp + "\n" + "s3cret"))
	require.Equal(t, base64.StdEncoding.EncodeToString(mac.Sum(nil)), query.Get("sign"))
}

func TestDingTalkReportsRejectedMessage(t *testing.T) {
	t.Parallel()

	server, _ := robotServer(t, `{"errcode":310000,"errmsg":"sign not match"}`)
	_, err := NewDingTalk(server.URL, "s3cret", true).Post(context.Background(), testChatMessage())
	require.ErrorContains(t, err, "sign not match")
}

func TestWeComPostsMarkdown(t *testing.T) {
	t.Parallel()

	server, raw := robotServer(t, `{"errcode":0,"errmsg":"ok"}`)
	_, err := NewWeCom(server.URL).Post(context.Background(), testChatMessage())
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(*raw, &body))
	require.Equal(t, "markdown", body["msgtype"])
	markdown, ok := body["markdown"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, markdown["content"], "Data quality: no nulls failed")
}

func TestWeComReportsRejectedMessage(t *testing.T) {
	t.Parallel()

	server, _ := robotServer(t, `{"errcode":93000,"errmsg":"invalid webhook url"}`)
	_, err := NewWeCom(server.URL).Post(context.Background(), testChatMessage())
	require.ErrorContains(t, err, "invalid webhook url")
}

func TestFeishuPostsText(t *testing.T) {
	t.Parallel()

	server, raw := robotServer(t, `{"code":0,"msg":"success"}`)
	_, err := NewFeishu(server.URL, "", false).Post(context.Background(), testChatMessage())
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(*raw, &body))
	require.Equal(t, "text", body["msg_type"])
	content, ok := body["content"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, content["text"], "Data quality: no nulls failed")
}

// A bot may still answer with the legacy StatusCode shape.
func TestFeishuAcceptsTheLegacyAnswerShape(t *testing.T) {
	t.Parallel()

	server, _ := robotServer(t, `{"StatusCode":0,"StatusMessage":"success"}`)
	_, err := NewFeishu(server.URL, "", false).Post(context.Background(), testChatMessage())
	require.NoError(t, err)
}

func TestFeishuReportsRejectedMessage(t *testing.T) {
	t.Parallel()

	server, _ := robotServer(t, `{"code":19021,"msg":"sign match fail"}`)
	_, err := NewFeishu(server.URL, "s3cret", true).Post(context.Background(), testChatMessage())
	require.ErrorContains(t, err, "sign match fail")
}

// Feishu signs with base64(HMAC-SHA256(key="<timestamp>\n<secret>", data="")).
func TestFeishuSignsTheRequest(t *testing.T) {
	t.Parallel()

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer server.Close()

	_, err := NewFeishu(server.URL, "s3cret", true).Post(context.Background(), testChatMessage())
	require.NoError(t, err)
	timestamp, ok := body["timestamp"].(string)
	require.True(t, ok)
	require.NotEmpty(t, timestamp)
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+"s3cret"))
	_, _ = mac.Write(nil)
	require.Equal(t, base64.StdEncoding.EncodeToString(mac.Sum(nil)), body["sign"])
}

// The endpoint is the credential for a robot, so a transport error must not echo
// it into the delivery's last_error or the test-send response.
func TestChatsSanitizeTheEndpointInErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL + "/robot/send?access_token=verysecret"
	server.Close()

	for name, receiver := range map[string]Receiver{
		"dingtalk": NewDingTalk(endpoint, "", false),
		"wecom":    NewWeCom(endpoint),
		"feishu":   NewFeishu(endpoint, "", false),
	} {
		_, err := receiver.Post(context.Background(), testChatMessage())
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), "verysecret", name)
		require.NotContains(t, err.Error(), endpoint, name)
	}
}

// A redirect is not followed: the endpoint chose the target, and re-posting to it
// would hand the robot token that sits in the URL to another host.
func TestChatsDoNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var targetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits++
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	for name, receiver := range map[string]Receiver{
		"dingtalk": NewDingTalk(redirect.URL, "", false),
		"wecom":    NewWeCom(redirect.URL),
		"feishu":   NewFeishu(redirect.URL, "", false),
	} {
		_, err := receiver.Post(context.Background(), testChatMessage())
		require.Error(t, err, name)
	}
	require.Zero(t, targetHits, "the redirect target must never be contacted")
}
