package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

// LLM feature labels for the three data-quality analyses. The agent loop records
// one LLM call per invocation with the feature it was given, so these are the only
// place the data-quality paths label their calls.
const (
	qualityLLMFeatureRuleDraft     = "dq_rule_draft"
	qualityLLMFeatureExplainResult = "dq_explain_result"
	qualityLLMFeatureColumnLabels  = "dq_column_labels"
)

// qualityLLMConfig resolves the provider and settings one data quality analysis
// runs with. The settings are part of the contract for callers that also bound
// their input with them (the label analysis caps columns per call).
//
// The analysis is off by default: the total switch, a usable provider and the
// workspace allowlist are all checked here, so the prompt handlers can return
// the error unchanged.
//
//nolint:unparam // the settings result is the helper's contract, not dead weight
func (s *DataQualityService) qualityLLMConfig(ctx context.Context) (llm.ResolvedConfig, dataquality.Settings, error) {
	var empty llm.ResolvedConfig
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return empty, settings, errLoadQualitySettings(err)
	}
	if !settings.EnableLLMAnalysis {
		return empty, settings, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("data quality LLM analysis is disabled in the workspace settings"))
	}
	if s.llmRegistry == nil {
		return empty, settings, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no LLM registry is configured"))
	}
	configs, err := s.llmRegistry.ListEnabled(ctx)
	if err != nil {
		return empty, settings, connect.NewError(connect.CodeFailedPrecondition,
			errors.Wrap(err, "failed to list the enabled LLM provider profiles"))
	}
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return empty, settings, connect.NewError(connect.CodeInternal,
			errors.Wrap(err, "failed to read the workspace LLM allowlist"))
	}
	config, err := llm.ResolveConfig(configs, setting.GetAllowedLlmProviderProfiles(), settings.LLMProviderProfile)
	if err != nil {
		return empty, settings, err
	}
	return *config, settings, nil
}

// qualityJSONCompletion runs one non-agent completion and returns its text. The
// prompt asks for a single JSON document, so the call fixes temperature at zero
// and asks the provider for JSON when it supports the mode; a provider that
// ignores the mode still answers something the strict parser can read.
//
// feature labels the call in metrics; calls, duration and tokens are recorded by
// the agent loop, not here.
func (s *DataQualityService) qualityJSONCompletion(ctx context.Context, config llm.ResolvedConfig, feature, systemPrompt, userPrompt string, maxOutputTokens int) (string, *llm.Usage, error) {
	if maxOutputTokens <= 0 {
		maxOutputTokens = qualityLLMMaxOutputTokens
	}
	zero := 0.0
	events := llm.RunAgentLoop(ctx, llm.AgentConfig{
		Provider:       config,
		Feature:        feature,
		Metrics:        s.metrics,
		SystemPrompt:   systemPrompt,
		UserPrompt:     userPrompt,
		MaxTurns:       1,
		MaxTokens:      maxOutputTokens,
		Temperature:    &zero,
		ResponseFormat: llm.JSONObjectResponseFormat(),
		IncludeUsage:   true,
	})
	var (
		builder strings.Builder
		usage   *llm.Usage
	)
	for event := range events {
		switch event.Type {
		case llm.AgentEventContent:
			_, _ = builder.WriteString(event.Content)
		case llm.AgentEventUsage:
			if event.Usage == nil {
				continue
			}
			if usage == nil {
				usage = &llm.Usage{}
			}
			usage.PromptTokens += event.Usage.PromptTokens
			usage.CompletionTokens += event.Usage.CompletionTokens
			usage.TotalTokens += event.Usage.TotalTokens
		case llm.AgentEventError:
			if event.Error != nil {
				return "", usage, connect.NewError(connect.CodeInternal, errors.Wrap(event.Error, "the LLM analysis failed"))
			}
			return "", usage, connect.NewError(connect.CodeInternal, errors.New("the LLM analysis failed"))
		case llm.AgentEventTurnStart, llm.AgentEventToolStart, llm.AgentEventToolEnd, llm.AgentEventAgentEnd:
			// One turn, no tools: nothing to do for these events.
		default:
			// An event kind added later needs no handling here until it matters.
		}
	}
	if builder.Len() == 0 {
		return "", usage, connect.NewError(connect.CodeInternal, errors.New("the LLM returned an empty response"))
	}
	return builder.String(), usage, nil
}

// qualityLLMProviderFacts returns the provider/model/prompt-version facts a
// suggestion or cache row records.
func qualityLLMProviderFacts(config llm.ResolvedConfig, promptVersion string) (provider, model, version string) {
	return config.ProfileName, config.ModelName, promptVersion
}
