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
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
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

	scopePrefix := req.Msg.ScopePrefix
	if scopePrefix == "" && metaGUID != "" {
		scopePrefix = metaGUIScope(metaGUID)
	}
	instanceID := scopeInstanceID(scopePrefix)

	cacheKey := explainSQLCacheKey(cacheIdentity, scopePrefix, resolvedConfig.ProfileName, resolvedConfig.ModelName)

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
	var ctxObjects *llm.SchemaContext
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

	// Build tools.
	tools := llm.ExplainSQLTools()
	executor := func(tc llm.ToolCall) ([]llm.ToolResult, error) {
		return s.executeExplainTool(ctx, tc, scopePrefix, instanceID)
	}

	// Run agent loop.
	var fullResponse strings.Builder
	cfg := llm.AgentConfig{
		Provider:             *resolvedConfig,
		SystemPrompt:         buildSystemPrompt(sqlText, metaGUID, metaType, ctxObjects),
		UserPrompt:           sqlText,
		Tools:                tools,
		Executor:             executor,
		MaxTurns:             llm.DefaultMaxTurns,
		MaxConversationBytes: llm.DefaultMaxConversationBytes,
	}
	if s.registry.DebugEnabled() {
		cfg.DebugLogger = llm.NewDBDebugLogger(s.store, resolvedConfig.ProfileName, resolvedConfig.ModelName)
	}

	// The producer stops as soon as this context is cancelled: returning from
	// this handler (a failed stream.Send, a client disconnect) must not leave the
	// agent goroutine blocked forever on a full channel.
	agentCtx, cancelAgent := context.WithCancel(ctx)
	defer cancelAgent()

	for evt := range llm.RunAgentLoop(agentCtx, cfg) {
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
// identity it was built for, and to the provider/model that produced it. The
// old key was the bare SQL or metadata hash, so two instances with the same SQL
// text shared an answer and switching provider returned the stale one.
func explainSQLCacheKey(cacheIdentity, scopePrefix, provider, model string) string {
	return fmt.Sprintf("%s|scope:%s|provider:%s|model:%s", cacheIdentity, scopePrefix, provider, model)
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

// isMySQLEngine reports whether names are addressed as database.table rather
// than schema.table. Keep it in sync with the engines the MySQL driver serves.
func isMySQLEngine(engine storepb.Engine) bool {
	switch engine {
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB, storepb.Engine_OCEANBASE:
		return true
	default:
		return false
	}
}

func resolveObjectIdentifier(name string, scopePrefix string, isMySQL bool) model.ObjectIdentifier {
	objID := model.StrToObjectIdentifier(name)

	if isMySQL && objID.Database == "" && objID.Schema != "" {
		objID.Database = objID.Schema
		objID.Schema = ""
	}

	parts := strings.Split(scopePrefix, ";")
	objID.InstanceID = scopeInstanceID(scopePrefix)

	if objID.Database == "" && len(parts) >= 2 && parts[1] != "" {
		objID.Database = parts[1]
	}
	if objID.Schema == "" && len(parts) >= 3 && parts[2] != "" {
		objID.Schema = parts[2]
	}

	return objID
}

// ---- Context building ----

func (s *ExplainSQLService) buildContextFromLineage(ctx context.Context, metaGUID string, metaType storepb.MetaType) (*llm.SchemaContext, error) {
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

func (s *ExplainSQLService) buildContextFromSQL(ctx context.Context, scopePrefix string, sqlText string) (*llm.SchemaContext, error) {
	instanceID := scopeInstanceID(scopePrefix)

	inst, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.Metadata == nil {
		return &llm.SchemaContext{}, nil
	}
	engine := inst.Metadata.Engine

	// A relation that cannot be analyzed (unsupported SQL, no registered
	// analyzer) is not a store failure: log it and continue with an empty
	// context.
	relations, err := lineage.GetAnalyzeRelation(ctx, engine, sqlText)
	if err != nil {
		slog.Debug("Failed to analyze SQL relations; continuing without schema context", log.WithError(err))
	}
	if len(relations) == 0 {
		return &llm.SchemaContext{}, nil
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

func (s *ExplainSQLService) fetchObjectsByGUIDs(ctx context.Context, guids []string) (*llm.SchemaContext, error) {
	if len(guids) == 0 {
		return &llm.SchemaContext{}, nil
	}

	// Keep the GUID next to the metadata it was resolved from: the pairs used to
	// be rebuilt positionally from the request list, so one failed lookup
	// shifted every following GUID onto the wrong metadata. A store failure is
	// propagated rather than skipped, so a DB outage is not reported as absent
	// metadata.
	var metas []*storepb.StoredMetadata
	matchedGUIDs := make([]string, 0, len(guids))
	for _, guid := range guids {
		list, err := s.store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
			GUID: &guid,
		})
		if err != nil {
			return nil, err
		}
		if len(list) == 0 || list[0].Metadata == nil {
			continue
		}
		metas = append(metas, list[0].Metadata)
		matchedGUIDs = append(matchedGUIDs, guid)
	}

	if len(metas) == 0 {
		return &llm.SchemaContext{}, nil
	}

	ctxObj := llm.BuildContextFromMetadata(metas, matchedGUIDs)
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

// ---- Tool execution ----

func (s *ExplainSQLService) executeExplainTool(ctx context.Context, tc llm.ToolCall, scopePrefix, instanceID string) ([]llm.ToolResult, error) {
	switch tc.Function.Name {
	case "get_object_schema":
		return s.toolGetObjectSchema(ctx, tc, scopePrefix, instanceID)
	case "search_objects":
		return s.toolSearchObjects(ctx, tc, scopePrefix)
	default:
		return nil, fmt.Errorf("unknown tool: %s", tc.Function.Name)
	}
}

func (s *ExplainSQLService) toolGetObjectSchema(ctx context.Context, tc llm.ToolCall, scopePrefix, instanceID string) ([]llm.ToolResult, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("invalid arguments for get_object_schema: %w", err)
	}

	engine, err := s.getScopeEngine(ctx, instanceID)
	if err != nil {
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: fmt.Sprintf(`{"error": "failed to resolve engine: %s"}`, err.Error())}}, nil
	}

	objID := resolveObjectIdentifier(args.Name, scopePrefix, isMySQLEngine(engine))
	guid := objID.GUID()

	list, err := s.store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
		GUID: &guid,
	})
	if err != nil {
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: fmt.Sprintf(`{"error": "failed to look up object: %s"}`, err.Error())}}, nil
	}
	if len(list) == 0 || list[0].Metadata == nil {
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: fmt.Sprintf(`{"error": "no object found matching '%s'"}`, args.Name)}}, nil
	}

	obj := llm.BuildContextFromMetadata([]*storepb.StoredMetadata{list[0].Metadata}, []string{guid})
	if len(obj.Objects) == 0 {
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: fmt.Sprintf(`{"error": "no object found matching '%s'"}`, args.Name)}}, nil
	}

	o := obj.Objects[0]
	sqlPreview := o.SQLText
	if len(sqlPreview) > 500 {
		sqlPreview = sqlPreview[:500] + "..."
	}

	resultJSON, _ := json.Marshal([]map[string]any{{
		"name":        o.Name,
		"type":        o.MetaType.String(),
		"schema":      o.SchemaName,
		"database":    o.DBName,
		"columns":     o.Columns,
		"indexes":     o.Indexes,
		"sql_preview": sqlPreview,
	}})

	return []llm.ToolResult{{ToolCallID: tc.ID, Content: string(resultJSON)}}, nil
}

func (s *ExplainSQLService) toolSearchObjects(ctx context.Context, tc llm.ToolCall, scopePrefix string) ([]llm.ToolResult, error) {
	var args struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("invalid arguments for search_objects: %w", err)
	}
	if strings.TrimSpace(args.Keyword) == "" {
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: `{"error": "keyword is required"}`}}, nil
	}

	// An empty scope_prefix means no instance was selected; the search then runs
	// across all instances instead of matching nothing.
	list, err := s.store.SearchMetaRegistryResource(ctx, &store.SearchMetaRegistryResourceMessage{
		SearchStr:  args.Keyword,
		GUIDPrefix: &scopePrefix,
		Limit:      20,
	})
	if err != nil {
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: fmt.Sprintf(`{"error": "search failed: %s"}`, err.Error())}}, nil
	}
	if len(list) == 0 {
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: `{"objects": []}`}}, nil
	}

	type objSummary struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Schema   string `json:"schema,omitempty"`
		Database string `json:"database,omitempty"`
	}

	matches := make([]objSummary, 0, len(list))
	for _, item := range list {
		if item.Metadata == nil {
			continue
		}
		obj := llm.BuildContextFromMetadata([]*storepb.StoredMetadata{item.Metadata}, []string{item.GUID})
		if len(obj.Objects) == 0 {
			continue
		}
		matches = append(matches, objSummary{
			Name:     obj.Objects[0].Name,
			Type:     obj.Objects[0].MetaType.String(),
			Schema:   obj.Objects[0].SchemaName,
			Database: obj.Objects[0].DBName,
		})
	}

	if len(matches) == 0 {
		return []llm.ToolResult{{ToolCallID: tc.ID, Content: `{"objects": []}`}}, nil
	}

	resultJSON, _ := json.Marshal(map[string]any{"objects": matches})
	return []llm.ToolResult{{ToolCallID: tc.ID, Content: string(resultJSON)}}, nil
}

func (s *ExplainSQLService) getScopeEngine(ctx context.Context, instanceID string) (storepb.Engine, error) {
	if instanceID == "" {
		return storepb.Engine_ENGINE_UNSPECIFIED, errors.New("no instance selected")
	}
	inst, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return storepb.Engine_ENGINE_UNSPECIFIED, err
	}
	if inst == nil || inst.Metadata == nil {
		return storepb.Engine_ENGINE_UNSPECIFIED, fmt.Errorf("instance not found: %s", instanceID)
	}
	return inst.Metadata.Engine, nil
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
