package metadata

// ExplainSQL's two tools, in the shared catalog: the object schema and the object
// search. They keep their original model-facing names, schemas and compact JSON
// projections; only the implementation moved here so ExplainSQL no longer owns a
// second tool layer.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ExplainSchemaContext holds the pre-built schema information the ExplainSQL
// prompt renders: the objects the statement reads, with their columns.
type ExplainSchemaContext struct {
	Objects []ExplainSchemaObject
}

// ExplainSchemaObject describes a database object (table, view, procedure, etc.).
type ExplainSchemaObject struct {
	GUID       string
	Name       string
	SchemaName string
	DBName     string
	MetaType   storepb.MetaType
	SQLText    string // DDL or definition
	Columns    []ExplainColumnInfo
	Indexes    []string
}

// ExplainColumnInfo describes a column.
type ExplainColumnInfo struct {
	Name     string
	Type     string
	Nullable bool
	Comment  string
}

// BuildExplainSchemaContext builds the prompt context from the metadata objects.
func BuildExplainSchemaContext(metas []*storepb.StoredMetadata, guids []string) *ExplainSchemaContext {
	ctx := &ExplainSchemaContext{}
	for i, meta := range metas {
		guid := ""
		if i < len(guids) {
			guid = guids[i]
		}
		obj := convertToExplainSchemaObject(guid, meta)
		if obj != nil {
			ctx.Objects = append(ctx.Objects, *obj)
		}
	}
	return ctx
}

func convertToExplainSchemaObject(guid string, meta *storepb.StoredMetadata) *ExplainSchemaObject {
	obj := &ExplainSchemaObject{GUID: guid}

	switch {
	case meta.GetTableMetadata() != nil:
		t := meta.GetTableMetadata()
		obj.Name = t.Name
		obj.MetaType = storepb.MetaType_TABLE
		for _, c := range t.Columns {
			obj.Columns = append(obj.Columns, ExplainColumnInfo{
				Name:     c.Name,
				Type:     c.Type,
				Nullable: c.Nullable,
				Comment:  c.Comment,
			})
		}
		for _, idx := range t.Indexes {
			obj.Indexes = append(obj.Indexes, idx.Name)
		}

	case meta.GetViewMetadata() != nil:
		v := meta.GetViewMetadata()
		obj.Name = v.Name
		obj.MetaType = storepb.MetaType_VIEW
		obj.SQLText = v.Definition
		for _, c := range v.Columns {
			obj.Columns = append(obj.Columns, ExplainColumnInfo{
				Name:     c.Name,
				Type:     c.Type,
				Nullable: c.Nullable,
				Comment:  c.Comment,
			})
		}

	case meta.GetMaterializedViewMetadata() != nil:
		mv := meta.GetMaterializedViewMetadata()
		obj.Name = mv.Name
		obj.MetaType = storepb.MetaType_MATERIALIZED_VIEW
		obj.SQLText = mv.Definition

	case meta.GetFunctionMetadata() != nil:
		f := meta.GetFunctionMetadata()
		obj.Name = f.Name
		obj.MetaType = storepb.MetaType_FUNCTION
		obj.SQLText = f.Definition

	case meta.GetProcedureMetadata() != nil:
		p := meta.GetProcedureMetadata()
		obj.Name = p.Name
		obj.MetaType = storepb.MetaType_PROCEDURE
		obj.SQLText = p.Definition

	case meta.GetExternalTableMetadata() != nil:
		et := meta.GetExternalTableMetadata()
		obj.Name = et.Name
		obj.MetaType = storepb.MetaType_EXTERNAL_TABLE
		for _, c := range et.Columns {
			obj.Columns = append(obj.Columns, ExplainColumnInfo{
				Name: c.Name,
				Type: c.Type,
			})
		}

	default:
		return nil
	}

	parts := common.SplitMetaGUID(guid)
	if len(parts) >= 2 {
		obj.DBName = parts[1]
	}
	if schema, ok := common.GetSchemaFromGUID(guid); ok {
		obj.SchemaName = schema
	}

	return obj
}

// ExplainStore is the slice of the store the ExplainSQL tools read.
type ExplainStore interface {
	ListMetaRegistry(ctx context.Context, find *store.FindMetaRegistryResourceMessage) ([]*store.MetaRegistryResource, error)
	SearchMetaRegistryResource(ctx context.Context, find *store.SearchMetaRegistryResourceMessage) ([]*store.MetaRegistryResource, error)
	GetInstance(ctx context.Context, find *store.FindInstanceMessage) (*store.InstanceMessage, error)
}

// ExplainTools runs ExplainSQL's tools over the registry.
type ExplainTools struct {
	store       ExplainStore
	scopePrefix string
	instanceID  string
}

// NewExplainTools builds ExplainSQL's tool implementation for one request scope.
func NewExplainTools(explainStore ExplainStore, scopePrefix, instanceID string) *ExplainTools {
	return &ExplainTools{store: explainStore, scopePrefix: scopePrefix, instanceID: instanceID}
}

// NewExplainToolSet resolves ExplainSQL's two tool declarations.
func NewExplainToolSet(explainStore ExplainStore, scopePrefix, instanceID string) (*tool.Set, error) {
	tools := NewExplainTools(explainStore, scopePrefix, instanceID)
	read := tool.Meta{
		Group:        "metadata",
		Scope:        tool.ScopeProductOnly,
		Authz:        tool.AuthzPermission,
		ReadOnly:     true,
		Permission:   string(permission.DatabasesRead),
		ParallelSafe: true,
	}
	registry := tool.NewRegistry()
	err := registry.Register(tool.Tool{
		Name:        "get_object_schema",
		Description: "Get detailed schema information for a database object (table, view, function, procedure, etc.). Use this when the SQL references an object and you need to understand its structure (columns, types, indexes).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The object name. Can be unqualified (e.g. 'users') or qualified (e.g. 'public.users').",
				},
			},
			"required": []string{"name"},
		},
		Meta: read,
		Execute: func(ctx context.Context, call tool.Call, _ *tool.Invocation) (tool.Result, error) {
			content, err := tools.GetObjectSchema(ctx, string(call.Arguments))
			if err != nil {
				return tool.Result{}, err
			}
			return tool.Result{Content: content}, nil
		},
	})
	if err != nil {
		return nil, err
	}
	err = registry.Register(tool.Tool{
		Name:        "search_objects",
		Description: "Search for database objects by keyword. Returns a list of matching object names and types. Use this to discover what objects exist before calling get_object_schema.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keyword": map[string]any{
					"type":        "string",
					"description": "Keyword to search for. Matches object names, column names, and SQL definitions.",
				},
			},
			"required": []string{"keyword"},
		},
		Meta: read,
		Execute: func(ctx context.Context, call tool.Call, _ *tool.Invocation) (tool.Result, error) {
			content, err := tools.SearchObjects(ctx, string(call.Arguments))
			if err != nil {
				return tool.Result{}, err
			}
			return tool.Result{Content: content}, nil
		},
	})
	if err != nil {
		return nil, err
	}
	return registry.Resolve([]string{"get_object_schema", "search_objects"})
}

// GetObjectSchema returns one object's compact schema JSON. A lookup failure is
// reported in band, as before: the model reads it and can try another name.
func (t *ExplainTools) GetObjectSchema(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments for get_object_schema: %w", err)
	}

	engine, err := t.getScopeEngine(ctx)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to resolve engine: %s"}`, err.Error()), nil
	}

	objID := resolveObjectIdentifier(args.Name, t.scopePrefix, isMySQLEngine(engine))
	guid := objID.GUID()

	list, err := t.store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &guid})
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to look up object: %s"}`, err.Error()), nil
	}
	if len(list) == 0 || list[0].Metadata == nil {
		return fmt.Sprintf(`{"error": "no object found matching '%s'"}`, args.Name), nil
	}

	obj := BuildExplainSchemaContext([]*storepb.StoredMetadata{list[0].Metadata}, []string{guid})
	if len(obj.Objects) == 0 {
		return fmt.Sprintf(`{"error": "no object found matching '%s'"}`, args.Name), nil
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
	return string(resultJSON), nil
}

// SearchObjects returns matching objects' compact JSON.
func (t *ExplainTools) SearchObjects(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Keyword string `json:"keyword"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments for search_objects: %w", err)
	}
	if strings.TrimSpace(args.Keyword) == "" {
		return `{"error": "keyword is required"}`, nil
	}

	scopePrefix := t.scopePrefix
	list, err := t.store.SearchMetaRegistryResource(ctx, &store.SearchMetaRegistryResourceMessage{
		SearchStr:  args.Keyword,
		GUIDPrefix: &scopePrefix,
		Limit:      20,
	})
	if err != nil {
		return fmt.Sprintf(`{"error": "search failed: %s"}`, err.Error()), nil
	}
	if len(list) == 0 {
		return `{"objects": []}`, nil
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
		obj := BuildExplainSchemaContext([]*storepb.StoredMetadata{item.Metadata}, []string{item.GUID})
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
		return `{"objects": []}`, nil
	}
	resultJSON, _ := json.Marshal(map[string]any{"objects": matches})
	return string(resultJSON), nil
}

// getScopeEngine resolves the instance the request is scoped to.
func (t *ExplainTools) getScopeEngine(ctx context.Context) (storepb.Engine, error) {
	if t.instanceID == "" {
		return storepb.Engine_ENGINE_UNSPECIFIED, errors.New("no instance selected")
	}
	inst, err := t.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &t.instanceID})
	if err != nil {
		return storepb.Engine_ENGINE_UNSPECIFIED, err
	}
	if inst == nil || inst.Metadata == nil {
		return storepb.Engine_ENGINE_UNSPECIFIED, fmt.Errorf("instance not found: %s", t.instanceID)
	}
	return inst.Metadata.Engine, nil
}

// isMySQLEngine reports whether names are addressed as database.table rather than
// schema.table. Keep it in sync with the engines the MySQL driver serves.
func isMySQLEngine(engine storepb.Engine) bool {
	switch engine {
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB, storepb.Engine_OCEANBASE:
		return true
	default:
		return false
	}
}

// scopeInstanceID reads the instance segment out of a scope prefix.
func scopeInstanceID(scopePrefix string) string {
	parts := strings.Split(scopePrefix, common.MetaGUIDSplit)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
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
