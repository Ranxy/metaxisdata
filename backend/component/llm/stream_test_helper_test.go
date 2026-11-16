package llm

// streamRequestForTest adapts the loop config the transport tests build onto the
// transport's own request shape.
func streamRequestForTest(cfg AgentConfig, messages []Message) StreamRequest {
	return StreamRequest{
		Provider:       cfg.Provider,
		Messages:       messages,
		Tools:          cfg.Tools,
		MaxTokens:      cfg.MaxTokens,
		Temperature:    cfg.Temperature,
		ResponseFormat: cfg.ResponseFormat,
		IncludeUsage:   cfg.IncludeUsage,
		DebugLogger:    cfg.DebugLogger,
	}
}
