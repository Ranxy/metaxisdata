package mcp

import (
	"context"
	_ "embed"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// guidePromptName is what a client asks for to pull the full guide.
const guidePromptName = "metaxisdata_guide"

// guideContent is the same guidance the server instructions summarize, in full.
// The instructions travel with a client's first request and have to stay short;
// a prompt is pulled on demand by whoever wants the whole story.
//
//go:embed guide.md
var guideContent string

// registerGuide publishes the guide as a prompt.
func (s *Server) registerGuide() {
	s.sdk.AddPrompt(&mcpsdk.Prompt{
		Name:        guidePromptName,
		Description: "How to query this metaxisdata deployment: how objects are addressed, which tool answers which question, and what a failure means.",
	}, func(_ context.Context, _ *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
		return &mcpsdk.GetPromptResult{
			Description: "Querying metaxisdata over MCP",
			Messages: []*mcpsdk.PromptMessage{{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: guideContent},
			}},
		}, nil
	})
}
