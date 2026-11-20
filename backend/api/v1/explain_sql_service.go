package v1

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type explainSection struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// explainSQLFeature labels this service's model calls in metrics.
const explainSQLFeature = "explain_sql"

// ExplainSQLService implements the explain SQL service.
type ExplainSQLService struct {
	v1connect.UnimplementedExplainSQLServiceHandler
	store    *store.Store
	registry *llm.Registry
	// lineage resolves the statement's relations against the analyzers the
	// process assembled, so the schema context is built the same way AnalyzeSQL
	// builds its answer.
	lineage *lineage.Analyzer
	// metrics records LLM call, token and cache outcomes; a nil value disables
	// instrumentation.
	metrics *metrics.Metrics
	// checker enforces each tool's IAM permission for the requesting user. Nil
	// skips the check (tests).
	checker tool.PermissionChecker
}

// NewExplainSQLService creates a new ExplainSQLService.
func NewExplainSQLService(st *store.Store, registry *llm.Registry, lineageAnalyzer *lineage.Analyzer, m *metrics.Metrics, checker tool.PermissionChecker) *ExplainSQLService {
	return &ExplainSQLService{store: st, registry: registry, lineage: lineageAnalyzer, metrics: m, checker: checker}
}

// ExplainSQL explains SQL using LLM.
func (s *ExplainSQLService) ExplainSQL(ctx context.Context, req *connect.Request[v1pb.ExplainSQLRequest], stream *connect.ServerStream[v1pb.ExplainSQLResponse]) error {
	// Resolve SQL text and the identity the explanation is valid for.
	sqlText, metaGUID, metaType, cacheIdentity, cacheType, err := s.resolveSource(ctx, req.Msg)
	if err != nil {
		return err
	}

	// The cache key must carry the provider/model and the instance scope, so
	// resolve them before consulting the cache. Two instances with identical SQL
	// text used to share one cached explanation.
	configs, err := s.registry.ListEnabled(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get LLM config"))
	}
	allowedProfiles, err := s.allowedLLMProfiles(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get the allowed LLM providers"))
	}
	resolvedConfig, err := resolveLLMConfig(configs, allowedProfiles, req.Msg.ProviderName)
	if err != nil {
		return err
	}

	scopePrefix := req.Msg.ScopePrefix
	if scopePrefix == "" && metaGUID != "" {
		scopePrefix = metaGUIScope(metaGUID)
	}
	instanceID := scopeInstanceID(scopePrefix)

	// The answer is written in the language the caller's UI is in, so the
	// language belongs in what a cached explanation is valid for: the same SQL
	// explained for a reader of another language is a different answer.
	language := normalizeLanguage(currentUserLanguage(ctx))

	cacheKey := explainSQLCacheKey(cacheIdentity, scopePrefix, resolvedConfig.ProfileName, resolvedConfig.ModelName, language)

	// Check cache.
	if !req.Msg.ForceRegenerate {
		cached, err := s.store.GetExplainSQLCache(ctx, cacheKey)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check cache"))
		}
		if cached != nil {
			var explanation struct {
				Summary  string           `json:"summary"`
				Sections []explainSection `json:"sections"`
			}
			if err := json.Unmarshal([]byte(cached.ExplanationJSON), &explanation); err == nil {
				md := buildMarkdownFromSections(explanation.Summary, explanation.Sections)
				_ = stream.Send(&v1pb.ExplainSQLResponse{
					Payload: &v1pb.ExplainSQLResponse_Content{Content: md},
				})
				_ = stream.Send(&v1pb.ExplainSQLResponse{
					Payload: &v1pb.ExplainSQLResponse_Metadata{
						Metadata: &v1pb.ExplainSQLMetadata{
							Summary:        explanation.Summary,
							Provider:       cached.Provider,
							Model:          cached.Model,
							CacheKey:       cached.CacheKey,
							CacheCreatedAt: cached.CreatedAt.Format(time.RFC3339),
							FromCache:      true,
						},
					},
				})
				return nil
			}
		}
	}

	// Build schema context. A store failure must surface instead of silently
	// degrading to an empty context, which the model reads as "no such object".
	var ctxObjects *explainSchemaContext
	if metaGUID != "" {
		ctxObjects, err = s.buildContextFromLineage(ctx, metaGUID, metaType)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to build lineage context"))
		}
	} else if scopePrefix != "" {
		ctxObjects, err = s.buildContextFromSQL(ctx, scopePrefix, sqlText)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to build SQL context"))
		}
	}

	// Build the request's tool set. The model sees the set's schemas and can only
	// run those same tools.
	set, err := s.explainSQLToolSet(scopePrefix, instanceID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to build the explain SQL tools"))
	}
	user, _ := GetUserFromContext(ctx)
	invocation := &tool.Invocation{
		Principal: user,
		Checker:   s.checker,
		Observer:  explainToolObserver{metrics: s.metrics},
	}
	executor := func(tc llm.ToolCall) ([]llm.ToolResult, error) {
		result := set.Execute(ctx, tool.Call{
			ID:        tc.ID,
			Name:      tc.GetName(),
			Arguments: json.RawMessage(tc.Function.Arguments),
		}, invocation)
		if result.IsError {
			if result.Error != nil {
				return nil, result.Error
			}
			return nil, errors.New(result.Content)
		}
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: result.Content}}, nil
	}

	// Run agent loop.
	var fullResponse strings.Builder
	// IncludeUsage stays off in v1: some OpenAI-compatible gateways reject the
	// stream_options field, and a rejected request is worse than missing usage.
	// The loop reports the missing usage, which is this path's compensation signal.
	cfg := agent.Config{
		Provider:             *resolvedConfig,
		Feature:              explainSQLFeature,
		Metrics:              s.metrics,
		SystemPrompt:         buildSystemPrompt(language, metaType, ctxObjects),
		UserPrompt:           sqlText,
		Tools:                agent.ToolDefs(set.Schemas()),
		Executor:             executor,
		MaxTurns:             agent.DefaultMaxTurns,
		MaxConversationBytes: agent.DefaultMaxConversationBytes,
	}
	if s.registry.DebugEnabled() {
		cfg.DebugLogger = llm.NewDBDebugLogger(s.store, resolvedConfig.ProfileName, resolvedConfig.ModelName)
	}

	// The producer stops as soon as this context is cancelled: returning from
	// this handler (a failed stream.Send, a client disconnect) must not leave the
	// agent goroutine blocked forever on a full channel.
	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()

	for evt := range agent.RunLoop(agentCtx, cfg) {
		switch evt.Type {
		case agent.EventError:
			return connect.NewError(connect.CodeInternal, evt.Error)
		case agent.EventTurnStart:
			_ = stream.Send(&v1pb.ExplainSQLResponse{
				Payload: &v1pb.ExplainSQLResponse_Progress{
					Progress: &v1pb.ExplainSQLProgress{
						Type: "thinking",
						Turn: int32(evt.Turn),
					},
				},
			})
		case agent.EventToolStart:
			_ = stream.Send(&v1pb.ExplainSQLResponse{
				Payload: &v1pb.ExplainSQLResponse_Progress{
					Progress: &v1pb.ExplainSQLProgress{
						Type:      "tool_start",
						Turn:      int32(evt.Turn),
						ToolName:  evt.ToolCall.GetName(),
						ToolInput: evt.ToolCall.Function.Arguments,
					},
				},
			})
		case agent.EventToolEnd:
			toolName := ""
			if evt.ToolCall != nil {
				toolName = evt.ToolCall.GetName()
			}
			_ = stream.Send(&v1pb.ExplainSQLResponse{
				Payload: &v1pb.ExplainSQLResponse_Progress{
					Progress: &v1pb.ExplainSQLProgress{
						Type:       "tool_end",
						Turn:       int32(evt.Turn),
						ToolName:   toolName,
						ToolOutput: evt.ToolResult,
						ToolError:  evt.ToolError,
					},
				},
			})
		case agent.EventContent:
			_, _ = fullResponse.WriteString(evt.Content)
			chunk := &v1pb.ExplainSQLResponse{
				Payload: &v1pb.ExplainSQLResponse_Content{Content: evt.Content},
			}
			if err := stream.Send(chunk); err != nil {
				return err
			}
		default:
		}
	}

	// Parse structured response and persist cache.
	responseText := fullResponse.String()
	summary, sectionsJSON := parseStructuredResponse(responseText)

	now := time.Now()
	cacheEntry := &store.ExplainSQLCacheRow{
		CacheKey: cacheKey,
		CacheType: func() int32 {
			if cacheType == "metadata" {
				return 0
			}
			return 1
		}(),
		MetaGUID:        metaGUID,
		Scope:           scopePrefix,
		SQLText:         sqlText,
		Provider:        resolvedConfig.ProfileName,
		Model:           resolvedConfig.ModelName,
		ExplanationJSON: toExplanationJSON(summary, sectionsJSON),
		CreatedAt:       now,
	}

	// Write the cache on a context detached from the request: the client often
	// disconnects right after reading the final chunk, and cancelling the write
	// would throw away an expensive result.
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), explainSQLCacheWriteTimeout)
	defer cancelWrite()
	if err := s.store.UpsertExplainSQLCache(writeCtx, cacheEntry); err != nil {
		slog.Warn("failed to cache SQL explanation", slog.String("cache_key", cacheKey), log.WithError(err))
	}

	_ = stream.Send(&v1pb.ExplainSQLResponse{
		Payload: &v1pb.ExplainSQLResponse_Metadata{
			Metadata: &v1pb.ExplainSQLMetadata{
				Summary:  summary,
				Provider: cacheEntry.Provider,
				Model:    cacheEntry.Model,
				CacheKey: cacheEntry.CacheKey,
			},
		},
	})

	return nil
}

// ---- Scope helpers ----

// explainSQLCacheWriteTimeout bounds the detached cache write.
const explainSQLCacheWriteTimeout = 5 * time.Second

// explainSQLCacheKey scopes a cached explanation to the instance (or object)
// identity it was built for, and to the provider/model and language that
// produced it. The old key was the bare SQL or metadata hash, so two instances
// with the same SQL text shared an answer and switching provider returned the
// stale one. The language is normalized by the caller, so an unset profile and
// an explicit default share one entry.
func explainSQLCacheKey(cacheIdentity, scopePrefix, provider, model, language string) string {
	return fmt.Sprintf("%s|scope:%s|provider:%s|model:%s|lang:%s", cacheIdentity, scopePrefix, provider, model, language)
}

func scopeInstanceID(scopePrefix string) string {
	if scopePrefix == "" {
		return ""
	}
	return common.SplitMetaGUID(scopePrefix)[0]
}

func metaGUIScope(metaGUID string) string {
	index := strings.LastIndex(metaGUID, common.MetaGUIDSplit)
	if index == -1 {
		return metaGUID
	}
	return metaGUID[:index]
}

// ---- Context building ----

func (s *ExplainSQLService) buildContextFromLineage(ctx context.Context, metaGUID string, metaType storepb.MetaType) (*explainSchemaContext, error) {
	lineageList, err := s.store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{
		MetaGUID: &metaGUID,
		MetaType: &metaType,
	})
	if err != nil {
		return nil, err
	}
	if len(lineageList) == 0 {
		return s.fetchObjectsByGUIDs(ctx, []string{metaGUID})
	}

	guidSet := make(map[string]bool)
	guidSet[metaGUID] = true
	for _, line := range lineageList {
		if isTableLikeType(line.SourceType) {
			guidSet[line.SourceGUID] = true
		}
		if isTableLikeType(line.TargetType) {
			guidSet[line.TargetGUID] = true
		}
	}

	guids := make([]string, 0, len(guidSet))
	for guid := range guidSet {
		guids = append(guids, guid)
	}
	return s.fetchObjectsByGUIDs(ctx, guids)
}

func (s *ExplainSQLService) buildContextFromSQL(ctx context.Context, scopePrefix string, sqlText string) (*explainSchemaContext, error) {
	instanceID := scopeInstanceID(scopePrefix)

	inst, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.Metadata == nil {
		return &explainSchemaContext{}, nil
	}
	engine := inst.Metadata.Engine

	// A relation that cannot be analyzed (unsupported SQL, no registered
	// analyzer) is not a store failure: log it and continue with an empty
	// context.
	relations, err := s.lineage.Analyze(ctx, engine, sqlText)
	if err != nil {
		slog.Debug("Failed to analyze SQL relations; continuing without schema context", log.WithError(err))
	}
	if len(relations) == 0 {
		return &explainSchemaContext{}, nil
	}

	guidSet := make(map[string]bool)
	for _, rel := range relations {
		if id := rel.Source.Table; id.Name != "" && !rel.IsTemp {
			id.InstanceID = instanceID
			guidSet[id.GUID()] = true
		}
		if id := rel.Target.Table; id.Name != "" && !rel.IsTemp {
			id.InstanceID = instanceID
			guidSet[id.GUID()] = true
		}
	}

	guids := make([]string, 0, len(guidSet))
	for guid := range guidSet {
		guids = append(guids, guid)
	}
	return s.fetchObjectsByGUIDs(ctx, guids)
}

func (s *ExplainSQLService) fetchObjectsByGUIDs(ctx context.Context, guids []string) (*explainSchemaContext, error) {
	if len(guids) == 0 {
		return &explainSchemaContext{}, nil
	}

	// Keep the GUID next to the metadata it was resolved from: the pairs used to
	// be rebuilt positionally from the request list, so one failed lookup
	// shifted every following GUID onto the wrong metadata. A store failure is
	// propagated rather than skipped, so a DB outage is not reported as absent
	// metadata.
	resolved, err := s.store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUIDs: &guids})
	if err != nil {
		return nil, err
	}
	byGUID := make(map[string]*storepb.StoredMetadata, len(resolved))
	for _, resource := range resolved {
		if resource.Metadata != nil {
			byGUID[resource.GUID] = resource.Metadata
		}
	}
	var metas []*storepb.StoredMetadata
	matchedGUIDs := make([]string, 0, len(guids))
	for _, guid := range guids {
		metadata, ok := byGUID[guid]
		if !ok {
			continue
		}
		metas = append(metas, metadata)
		matchedGUIDs = append(matchedGUIDs, guid)
	}

	if len(metas) == 0 {
		return &explainSchemaContext{}, nil
	}

	ctxObj := buildExplainSchemaContext(metas, matchedGUIDs)
	if len(ctxObj.Objects) > 10 {
		ctxObj.Objects = ctxObj.Objects[:10]
	}
	return ctxObj, nil
}

func isTableLikeType(mt storepb.MetaType) bool {
	switch mt {
	case storepb.MetaType_TABLE, storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW,
		storepb.MetaType_FUNCTION, storepb.MetaType_PROCEDURE, storepb.MetaType_EXTERNAL_TABLE:
		return true
	default:
		return false
	}
}

// allowedLLMProfiles returns the workspace's LLM provider allowlist. An empty
// list means every enabled profile is allowed.
func (s *ExplainSQLService) allowedLLMProfiles(ctx context.Context) ([]string, error) {
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, err
	}
	return setting.GetAllowedLlmProviderProfiles(), nil
}

// The provider-selection rules live in component/llm, which keeps the allowlist
// and the default in one place. Explain SQL is the only feature that applies the
// workspace allowlist: the data quality analyses and quality agents pick their
// profile in their own settings. These helpers keep the RPC's names and
// signatures — the handler and this package's tests call them — and delegate the
// rule itself to the shared implementation.

// findLLMConfig returns the enabled config the profile reference names, or nil.
func findLLMConfig(configs []llm.ResolvedConfig, profile string) *llm.ResolvedConfig {
	return llm.FindConfig(configs, profile)
}

// filterAllowedLLMConfigs keeps the configs whose profile is in the allowlist. An
// empty allowlist means no restriction.
func filterAllowedLLMConfigs(configs []llm.ResolvedConfig, allowed []string) []llm.ResolvedConfig {
	return llm.AllowedConfigs(configs, allowed)
}

// resolveLLMConfig picks the provider and model the explanation runs on. An
// explicitly requested profile must be allowed by the workspace setting; otherwise
// the first allowed one is the default. The errors are the RPC's, so the handler
// returns them unchanged.
func resolveLLMConfig(configs []llm.ResolvedConfig, allowed []string, providerName string) (*llm.ResolvedConfig, error) {
	return llm.ResolveConfig(configs, allowed, providerName)
}

// isLLMProfileAllowed reports whether a requested profile is inside the allowlist;
// an empty allowlist allows everything.
func isLLMProfileAllowed(profile string, allowed []string) bool {
	return llm.ProfileAllowed(profile, allowed)
}

// resolveSource returns the SQL to explain, the object it belongs to, the
// identity the cached answer is valid for before scoping, and the cache kind.
func (s *ExplainSQLService) resolveSource(ctx context.Context, req *v1pb.ExplainSQLRequest) (sqlText, metaGUID string, metaType storepb.MetaType, cacheIdentity, cacheType string, err error) {
	if req.SqlText != "" {
		hash := sha256.Sum256([]byte(req.SqlText))
		return req.SqlText, "", storepb.MetaType_UNSPECIFIED, fmt.Sprintf("sql:%x", hash), "custom", nil
	}

	if req.MetaGuid != "" {
		metas, listErr := s.store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
			GUID: &req.MetaGuid,
		})
		// A store failure is not the same as a missing row: reporting it as
		// NotFound hid outages behind a client error.
		if listErr != nil {
			return "", "", storepb.MetaType_UNSPECIFIED, "", "", connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up metadata %q: %v", req.MetaGuid, listErr))
		}
		if len(metas) == 0 {
			return "", "", storepb.MetaType_UNSPECIFIED, "", "", connect.NewError(connect.CodeNotFound, errors.Errorf("metadata not found for guid %q", req.MetaGuid))
		}

		meta := metas[0]
		sqlText = extractSQLFromMeta(meta.Metadata)
		if sqlText == "" {
			return "", "", storepb.MetaType_UNSPECIFIED, "", "", connect.NewError(connect.CodeInvalidArgument, errors.New("no SQL text available for this metadata type"))
		}

		if meta.MetaHash != nil {
			cacheIdentity = fmt.Sprintf("meta:%x", meta.MetaHash)
		} else {
			hash := sha256.Sum256([]byte(req.MetaGuid + sqlText))
			cacheIdentity = fmt.Sprintf("meta:%x", hash)
		}

		return sqlText, req.MetaGuid, meta.ObjectType, cacheIdentity, "metadata", nil
	}

	return "", "", storepb.MetaType_UNSPECIFIED, "", "", connect.NewError(connect.CodeInvalidArgument, errors.New("either sql_text or meta_guid is required"))
}

// buildSystemPrompt writes the explanation brief in the requested language: the
// headings and the instruction to follow them, so a reader of that language gets
// a wholly translated answer instead of prose in one language under headings in
// another. The language is normalized here as well, so the prompt cannot fall
// back to Chinese headings for an unrecognized tag.
func buildSystemPrompt(language string, metaType storepb.MetaType, ctx *explainSchemaContext) string {
	typeLabel := metaType.String()
	if metaType == storepb.MetaType_UNSPECIFIED {
		typeLabel = "SQL"
	}

	prompt := languagePrompts[normalizeLanguage(language)]
	contextText := buildContextText(ctx)
	return fmt.Sprintf(`You are an expert SQL analyst. Explain the following %s in detail.
Write the entire answer, headings included, in %s.

Start with a one-sentence summary. Then provide four sections using ## headings. Follow this exact structure:

## %s
(step-by-step breakdown: execution flow, data transformations, join order, filter conditions)

## %s
(list tables, views, functions, procedures with their columns, types, and relevant structure)

## %s
(performance issues, edge cases, null handling, missing indexes, security concerns)

## %s
(concrete improvements: index recommendations, query rewrites, partitioning, caching)

Rules:
- Write in %s
- Use markdown code blocks for SQL snippets
- Use bullet lists for enumeration
- Keep exactly four ## headings as specified above
- Do not add extra introductory or concluding text
- Do not wrap response in JSON or code fences

You have tools to query schema information — use them when needed.
%s`, typeLabel, prompt.name,
		prompt.headings[0], prompt.headings[1], prompt.headings[2], prompt.headings[3],
		prompt.name, contextText)
}

func buildContextText(ctx *explainSchemaContext) string {
	if ctx == nil || len(ctx.Objects) == 0 {
		return "\nNo schema context is available. Use the tools to query schema information."
	}

	var sb strings.Builder
	_, _ = sb.WriteString("\n\nThe following schema context is available:\n")
	for _, obj := range ctx.Objects {
		_, _ = fmt.Fprintf(&sb, "\n- **%s** (%s): ", obj.Name, obj.MetaType.String())
		if len(obj.Columns) > 0 {
			colNames := make([]string, len(obj.Columns))
			for i, c := range obj.Columns {
				colNames[i] = c.Name
			}
			_, _ = sb.WriteString(strings.Join(colNames, ", "))
		}
		if obj.SQLText != "" {
			preview := obj.SQLText
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			_, _ = fmt.Fprintf(&sb, "\n  SQL: %s", preview)
		}
	}
	return sb.String()
}

func extractSQLFromMeta(meta *storepb.StoredMetadata) string {
	switch {
	case meta.GetTableMetadata() != nil:
		return buildCreateTableSQL(meta.GetTableMetadata())
	case meta.GetViewMetadata() != nil:
		return meta.GetViewMetadata().Definition
	case meta.GetMaterializedViewMetadata() != nil:
		return meta.GetMaterializedViewMetadata().Definition
	case meta.GetFunctionMetadata() != nil:
		return meta.GetFunctionMetadata().Definition
	case meta.GetProcedureMetadata() != nil:
		return meta.GetProcedureMetadata().Definition
	default:
		return ""
	}
}

func buildCreateTableSQL(t *storepb.TableMetadata) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "CREATE TABLE %s (\n", t.Name)
	for i, c := range t.Columns {
		nullable := ""
		if !c.Nullable {
			nullable = " NOT NULL"
		}
		_, _ = fmt.Fprintf(&sb, "  %s %s%s", c.Name, c.Type, nullable)
		if i < len(t.Columns)-1 {
			_, _ = sb.WriteString(",\n")
		}
	}
	_, _ = sb.WriteString("\n);")
	return sb.String()
}

func parseStructuredResponse(text string) (summary string, sectionsJSON string) {
	text = strings.TrimSpace(text)

	// Split on every line that starts with "## ", including the very first
	// line. Looking only for "\n## " left the marker inside the first section
	// title (rendered as "## ## ...") and, when the model's first line was a
	// heading, kept the whole answer as the summary with an empty section.
	var summaryLines []string
	var contentLines []string
	var sections []explainSection
	currentTitle := ""
	flush := func() {
		if currentTitle == "" {
			return
		}
		sections = append(sections, explainSection{
			Title:   currentTitle,
			Content: strings.TrimSpace(strings.Join(contentLines, "\n")),
		})
		contentLines = nil
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if heading, ok := strings.CutPrefix(trimmed, "## "); ok {
			flush()
			currentTitle = strings.TrimSpace(heading)
			continue
		}
		if currentTitle == "" {
			summaryLines = append(summaryLines, line)
		} else {
			contentLines = append(contentLines, line)
		}
	}
	flush()

	summaryText := strings.TrimSpace(strings.Join(summaryLines, "\n"))
	if summaryText == "" {
		summaryText = "SQL Explanation"
	}

	if len(sections) == 0 {
		var legacy struct {
			Summary  string           `json:"summary"`
			Sections []explainSection `json:"sections"`
		}
		if err := json.Unmarshal([]byte(text), &legacy); err == nil && len(legacy.Sections) > 0 {
			sections = legacy.Sections
			if legacy.Summary != "" {
				summaryText = legacy.Summary
			}
		} else {
			sections = append(sections, explainSection{Title: "Explanation", Content: text})
		}
	}

	sectionsBytes, _ := json.Marshal(sections)
	return summaryText, string(sectionsBytes)
}

func toExplanationJSON(summary, sectionsJSON string) string {
	return fmt.Sprintf(`{"summary":%s,"sections":%s}`, jsonEscape(summary), sectionsJSON)
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func buildMarkdownFromSections(summary string, sections []explainSection) string {
	var sb strings.Builder
	_, _ = sb.WriteString(summary)
	for _, s := range sections {
		_, _ = sb.WriteString("\n\n## ")
		_, _ = sb.WriteString(s.Title)
		_, _ = sb.WriteString("\n\n")
		_, _ = sb.WriteString(s.Content)
	}
	return sb.String()
}
