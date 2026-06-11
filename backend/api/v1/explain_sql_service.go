package v1

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type explainSection struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// ExplainSQLService implements the explain SQL service.
type ExplainSQLService struct {
	v1connect.UnimplementedExplainSQLServiceHandler
	store    *store.Store
	registry *llm.Registry
}

// NewExplainSQLService creates a new ExplainSQLService.
func NewExplainSQLService(st *store.Store, registry *llm.Registry) *ExplainSQLService {
	return &ExplainSQLService{store: st, registry: registry}
}

// ExplainSQL explains SQL using LLM.
func (s *ExplainSQLService) ExplainSQL(ctx context.Context, req *connect.Request[v1pb.ExplainSQLRequest], stream *connect.ServerStream[v1pb.ExplainSQLResponse]) error {
	// Resolve SQL text and cache key.
	sqlText, metaGUID, metaType, cacheKey, cacheType, err := s.resolveSource(ctx, req.Msg)
	if err != nil {
		return err
	}

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
				sectionsJSON, _ := json.Marshal(explanation.Sections)
				_ = stream.Send(&v1pb.ExplainSQLResponse{
					Payload: &v1pb.ExplainSQLResponse_Metadata{
						Metadata: &v1pb.ExplainSQLMetadata{
							Summary:        explanation.Summary,
							SectionsJson:   string(sectionsJSON),
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

	// Get active LLM provider config.
	configs, err := s.registry.ListEnabled(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get LLM config"))
	}
	var resolvedConfig *llm.ResolvedConfig
	if req.Msg.ProviderName != "" {
		for _, c := range configs {
			if c.ProfileName == req.Msg.ProviderName {
				resolvedConfig = &c
				break
			}
		}
		if resolvedConfig == nil {
			return connect.NewError(connect.CodeNotFound, errors.Errorf("LLM profile %q not found", req.Msg.ProviderName))
		}
	} else {
		if len(configs) == 0 {
			return connect.NewError(connect.CodeFailedPrecondition, errors.New("no enabled LLM provider profiles"))
		}
		resolvedConfig = &configs[0]
	}

	// Build schema context from lineage.
	ctxObjects := s.buildSchemaContext(ctx, metaGUID, metaType, req.Msg.ScopePrefix)

	// Build tools.
	tools := llm.ExplainSQLTools()
	executor := func(tc llm.ToolCall) ([]llm.ToolResult, error) {
		return llm.ExecuteTool(tc, ctxObjects)
	}

	// Run agent loop.
	var fullResponse strings.Builder
	cfg := llm.AgentConfig{
		Provider:     *resolvedConfig,
		SystemPrompt: buildSystemPrompt(sqlText, metaGUID, metaType, ctxObjects),
		UserPrompt:   sqlText,
		Tools:        tools,
		Executor:     executor,
	}
	if s.registry.DebugEnabled() {
		cfg.DebugLogger = llm.NewDBDebugLogger(s.store, resolvedConfig.ProfileName, resolvedConfig.ModelName)
	}

	for evt := range llm.RunAgentLoop(ctx, cfg) {
		switch evt.Type {
		case llm.AgentEventError:
			return connect.NewError(connect.CodeInternal, evt.Error)
		case llm.AgentEventTurnStart:
			_ = stream.Send(&v1pb.ExplainSQLResponse{
				Payload: &v1pb.ExplainSQLResponse_Progress{
					Progress: &v1pb.ExplainSQLProgress{
						Type: "thinking",
						Turn: int32(evt.Turn),
					},
				},
			})
		case llm.AgentEventToolStart:
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
		case llm.AgentEventToolEnd:
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
		case llm.AgentEventContent:
			_, _ = fullResponse.WriteString(evt.Content)
			if err := stream.Send(&v1pb.ExplainSQLResponse{
				Payload: &v1pb.ExplainSQLResponse_Content{Content: evt.Content},
			}); err != nil {
				return err
			}
		case llm.AgentEventAgentEnd:
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
		SQLText:         sqlText,
		Provider:        resolvedConfig.ProfileName,
		Model:           resolvedConfig.ModelName,
		ExplanationJSON: toExplanationJSON(summary, sectionsJSON),
		CreatedAt:       now,
	}

	if err := s.store.UpsertExplainSQLCache(ctx, cacheEntry); err != nil {
		// Log but don't fail — the user already got the explanation.
		_ = err
	}

	_ = stream.Send(&v1pb.ExplainSQLResponse{
		Payload: &v1pb.ExplainSQLResponse_Metadata{
			Metadata: &v1pb.ExplainSQLMetadata{
				Summary:      summary,
				SectionsJson: sectionsJSON,
				Provider:     cacheEntry.Provider,
				Model:        cacheEntry.Model,
				CacheKey:     cacheEntry.CacheKey,
			},
		},
	})

	return nil
}

// resolveSource determines sql_text, cache_key, and cache_type from the request.
func (s *ExplainSQLService) resolveSource(ctx context.Context, req *v1pb.ExplainSQLRequest) (sqlText, metaGUID string, metaType storepb.MetaType, cacheKey, cacheType string, err error) {
	if req.SqlText != "" {
		hash := sha256.Sum256([]byte(req.SqlText))
		cacheKey = fmt.Sprintf("sql:%x", hash)
		return req.SqlText, "", storepb.MetaType_UNSPECIFIED, cacheKey, "custom", nil
	}

	if req.MetaGuid != "" {
		// Look up metadata from registry.
		metas, listErr := s.store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
			GUID: &req.MetaGuid,
		})
		if listErr != nil || len(metas) == 0 {
			return "", "", storepb.MetaType_UNSPECIFIED, "", "", connect.NewError(connect.CodeNotFound, errors.Errorf("metadata not found for guid %q", req.MetaGuid))
		}

		meta := metas[0]
		sqlText = extractSQLFromMeta(meta.Metadata)
		if sqlText == "" {
			return "", "", storepb.MetaType_UNSPECIFIED, "", "", connect.NewError(connect.CodeInvalidArgument, errors.New("no SQL text available for this metadata type"))
		}

		// Cache key is the meta_hash.
		if meta.MetaHash != nil {
			cacheKey = fmt.Sprintf("meta:%x", meta.MetaHash)
		} else {
			hash := sha256.Sum256([]byte(req.MetaGuid + sqlText))
			cacheKey = fmt.Sprintf("meta:%x", hash)
		}

		return sqlText, req.MetaGuid, meta.ObjectType, cacheKey, "metadata", nil
	}

	return "", "", storepb.MetaType_UNSPECIFIED, "", "", connect.NewError(connect.CodeInvalidArgument, errors.New("either sql_text or meta_guid is required"))
}

func (s *ExplainSQLService) buildSchemaContext(ctx context.Context, metaGUID string, _ storepb.MetaType, scopePrefix string) *llm.SchemaContext {
	if metaGUID == "" {
		if scopePrefix == "" {
			return &llm.SchemaContext{}
		}
		return s.buildSchemaContextFromScope(ctx, scopePrefix)
	}

	// Collect upstream and downstream GUIDs from column_lineage.
	relatedGUIDs := make(map[string]bool)
	relatedGUIDs[metaGUID] = true

	// Upstream: find all sources for this object.
	upstreamRows, err := s.store.QueryColumnLineageSources(context.Background(), metaGUID)
	if err == nil {
		for _, r := range upstreamRows {
			relatedGUIDs[r] = true
		}
	}

	// Downstream: find all targets for this object.
	downstreamRows, err := s.store.QueryColumnLineageTargets(context.Background(), metaGUID)
	if err == nil {
		for _, r := range downstreamRows {
			relatedGUIDs[r] = true
		}
	}

	// Fetch metadata for all related GUIDs.
	// (Ignore unknowable objects like PROCEDURE/FUNCTION without lineage — LLM can use tools.)
	guids := make([]string, 0, len(relatedGUIDs))
	for guid := range relatedGUIDs {
		guids = append(guids, guid)
	}

	var metas []*storepb.StoredMetadata
	for _, guid := range guids {
		list, err := s.store.ListMetaRegistry(context.Background(), &store.FindMetaRegistryResourceMessage{
			GUID: &guid,
		})
		if err != nil || len(list) == 0 {
			continue
		}
		metas = append(metas, list[0].Metadata)
	}

	ctxObj := llm.BuildContextFromMetadata(metas, guids)
	if len(ctxObj.Objects) > 10 {
		ctxObj.Objects = ctxObj.Objects[:10]
	}
	return ctxObj
}

var scopeObjectTypes = []storepb.MetaType{
	storepb.MetaType_TABLE,
	storepb.MetaType_VIEW,
	storepb.MetaType_MATERIALIZED_VIEW,
	storepb.MetaType_FUNCTION,
	storepb.MetaType_PROCEDURE,
}

func (s *ExplainSQLService) buildSchemaContextFromScope(ctx context.Context, scopePrefix string) *llm.SchemaContext {
	limit := 50
	list, err := s.store.ListMetaRegistryResource(ctx, &store.FindMetaRegistryResourceMessage{
		GUIDPrefix: &scopePrefix,
		Limit:      &limit,
	})
	if err != nil || len(list) == 0 {
		return &llm.SchemaContext{}
	}

	allowedTypes := make(map[storepb.MetaType]bool, len(scopeObjectTypes))
	for _, t := range scopeObjectTypes {
		allowedTypes[t] = true
	}

	var metas []*storepb.StoredMetadata
	var guids []string
	for _, item := range list {
		if !allowedTypes[item.ObjectType] {
			continue
		}
		metas = append(metas, item.Metadata)
		guids = append(guids, item.GUID)
	}

	ctxObj := llm.BuildContextFromMetadata(metas, guids)
	if len(ctxObj.Objects) > 10 {
		ctxObj.Objects = ctxObj.Objects[:10]
	}
	return ctxObj
}

func buildSystemPrompt(_ string, _ string, metaType storepb.MetaType, ctx *llm.SchemaContext) string {
	typeLabel := metaType.String()
	if metaType == storepb.MetaType_UNSPECIFIED {
		typeLabel = "SQL"
	}

	contextText := buildContextText(ctx)
	return fmt.Sprintf(`You are an expert SQL analyst. Explain the following %s in detail.

Start with a one-sentence summary. Then provide four sections using ## headings. Follow this exact structure:

## 执行逻辑
(step-by-step breakdown: execution flow, data transformations, join order, filter conditions)

## 涉及对象
(list tables, views, functions, procedures with their columns, types, and relevant structure)

## 潜在问题
(performance issues, edge cases, null handling, missing indexes, security concerns)

## 优化建议
(concrete improvements: index recommendations, query rewrites, partitioning, caching)

Rules:
- Use markdown code blocks for SQL snippets
- Use bullet lists for enumeration
- Keep exactly four ## headings as specified above
- Do not add extra introductory or concluding text
- Do not wrap response in JSON or code fences

You have tools to query schema information — use them when needed.
%s`, typeLabel, contextText)
}

func buildContextText(ctx *llm.SchemaContext) string {
	if len(ctx.Objects) == 0 {
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

	idx := strings.Index(text, "\n## ")
	var summaryText string
	if idx >= 0 {
		summaryText = strings.TrimSpace(text[:idx])
		text = text[idx+1:]
	} else {
		summaryText = strings.TrimSpace(text)
		text = ""
	}
	if summaryText == "" {
		summaryText = "SQL Explanation"
	}

	var sections []explainSection
	sectionParts := strings.Split(text, "\n## ")
	for _, part := range sectionParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lineEnd := strings.Index(part, "\n")
		var title, content string
		if lineEnd >= 0 {
			title = strings.TrimSpace(part[:lineEnd])
			content = strings.TrimSpace(part[lineEnd+1:])
		} else {
			title = strings.TrimSpace(part)
			content = ""
		}
		if title != "" {
			sections = append(sections, explainSection{Title: title, Content: content})
		}
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
