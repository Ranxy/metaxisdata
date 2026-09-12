package v1

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/plugin/schema"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func (s *DatabaseService) GetSchemaString(ctx context.Context, req *connect.Request[v1pb.GetSchemaStringRequest]) (*connect.Response[v1pb.MetadataSchemaString], error) {
	instanceGUID, ok := common.GetInstaceFromGUID(req.Msg.Guid)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid guid %q", req.Msg.Guid))
	}

	schemaName, ok := common.GetSchemaFromGUID(req.Msg.Guid)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid guid %q", req.Msg.Guid))
	}

	instance, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceGUID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get instance %q: %v", instanceGUID, err))
	}

	if instance == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q not found", instanceGUID))
	}

	meta, err := s.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &req.Msg.Guid, ObjectType: (*storepb.MetaType)(&req.Msg.MetaType)})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get meta registry %q: %v", req.Msg.Guid, err))
	}
	if meta == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("meta registry %q not found", req.Msg.Guid))
	}

	engine := instance.Metadata.GetEngine()

	switch req.Msg.MetaType {
	case v1pb.MetaType_TABLE:
		tableMeta := meta.Metadata.GetTableMetadata()
		if tableMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("table metadata is nil"))
		}

		// Get sequences that own this table from the same schema
		schemaPrefix := common.GUIDPrefix(req.Msg.Guid)
		sequences, err := s.getTableSequences(ctx, schemaPrefix, tableMeta.Name)
		if err != nil {
			return nil, err
		}

		schemaStr, err := schema.GetTableDefinition(engine, schemaName, tableMeta, sequences)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate table definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_VIEW:
		viewMeta := meta.Metadata.GetViewMetadata()
		if viewMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("view metadata is nil"))
		}

		schemaStr, err := schema.GetViewDefinition(engine, schemaName, viewMeta)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate view definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_MATERIALIZED_VIEW:
		mvMeta := meta.Metadata.GetMaterializedViewMetadata()
		if mvMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("materialized view metadata is nil"))
		}

		schemaStr, err := schema.GetMaterializedViewDefinition(engine, schemaName, mvMeta)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate materialized view definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_FUNCTION:
		funcMeta := meta.Metadata.GetFunctionMetadata()
		if funcMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("function metadata is nil"))
		}

		schemaStr, err := schema.GetFunctionDefinition(engine, schemaName, funcMeta)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate function definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_PROCEDURE:
		procMeta := meta.Metadata.GetProcedureMetadata()
		if procMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("procedure metadata is nil"))
		}

		schemaStr, err := schema.GetProcedureDefinition(engine, schemaName, procMeta)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate procedure definition: %v", err))
		}
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: schemaStr}), nil

	case v1pb.MetaType_MANUAL_SQL:
		manualSQLMeta := meta.Metadata.GetManualSqlMetadata()
		if manualSQLMeta == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("manual SQL metadata is nil"))
		}

		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: manualSQLMeta.SqlText}), nil

	default:
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("GetMetadataSchema is not implemented for this meta type"))
	}
}

func (s *DatabaseService) DiffMetadata(ctx context.Context, req *connect.Request[v1pb.DiffMetadataRequest]) (*connect.Response[v1pb.DiffMetadataResponse], error) {
	guid := req.Msg.GetGuid()
	if strings.TrimSpace(guid) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}

	// 1. Get instance engine from GUID
	instanceGUID, ok := common.GetInstaceFromGUID(guid)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid guid %q", guid))
	}
	instance, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceGUID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get instance %q", instanceGUID))
	}
	if instance == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("instance %q not found", instanceGUID))
	}
	engine := instance.Metadata.GetEngine()

	// 2. Rebuild DatabaseSchemaMetadata at source and target times
	sourceMeta, err := s.buildDatabaseSchemaAtTime(ctx, guid, req.Msg.GetSourceTime())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to build source schema"))
	}
	targetMeta, err := s.buildDatabaseSchemaAtTime(ctx, guid, req.Msg.GetTargetTime())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to build target schema"))
	}

	// 3. Compute diff
	diff, err := schema.GetDatabaseSchemaDiff(engine, sourceMeta, targetMeta)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to compute schema diff"))
	}
	if diff == nil {
		return connect.NewResponse(&v1pb.DiffMetadataResponse{
			DiffSummary: "No changes detected.",
			Ddl:         "",
		}), nil
	}

	// 4. Generate DDL
	ddl, err := schema.GenerateMigration(engine, diff)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate migration DDL"))
	}

	// 5. Build summary
	summary := buildDiffSummary(diff)

	return connect.NewResponse(&v1pb.DiffMetadataResponse{
		DiffSummary: summary,
		Ddl:         ddl,
	}), nil
}

func (s *DatabaseService) buildDatabaseSchemaAtTime(ctx context.Context, guid string, asOf *timestamppb.Timestamp) (*storepb.DatabaseSchemaMetadata, error) {
	var asOfTime time.Time
	if asOf != nil {
		asOfTime = asOf.AsTime()
	} else {
		asOfTime = time.Now()
	}

	// Determine the scope: database-level or schema-level
	parts := strings.Split(guid, common.MetaGUIDSplit)
	if len(parts) < 2 {
		return nil, errors.Errorf("guid %q is not deep enough (need at least instance;database)", guid)
	}

	result := &storepb.DatabaseSchemaMetadata{
		Name: parts[1], // database name
	}

	// Fetch schemas valid at asOfTime
	schemaObjects, err := s.store.ListMetaRegistryResourceAsOf(ctx, &store.FindMetaRegistryResourceMessage{
		GUIDPrefix: &guid,
		ObjectType: storepb.MetaType_SCHEMA.Enum(),
	}, asOfTime)
	if err != nil {
		return nil, err
	}

	for _, schemaObj := range schemaObjects {
		schemaMeta := schemaObj.Metadata.GetSchemaMetadata()
		if schemaMeta == nil {
			continue
		}

		// Rebuild this schema's contents
		schemaGUID := schemaObj.GUID
		rebuilt := s.rebuildSchemaContents(ctx, schemaGUID, schemaMeta, asOfTime)
		result.Schemas = append(result.Schemas, rebuilt)
	}

	// If no schemas found, try database-level reconstruction (for MySQL which has no schema)
	if len(result.Schemas) == 0 {
		// Use the guid directly to get tables at schema level
		dbMeta, err := s.rebuildDatabaseObjects(ctx, guid, asOfTime)
		if err != nil {
			return nil, err
		}
		if dbMeta != nil {
			result.Schemas = append(result.Schemas, dbMeta)
		}
	}

	return result, nil
}

func (s *DatabaseService) rebuildDatabaseObjects(ctx context.Context, guid string, asOfTime time.Time) (*storepb.SchemaMetadata, error) {
	result := &storepb.SchemaMetadata{Name: ""}

	// Fetch tables
	tableType := storepb.MetaType_TABLE
	tables, err := s.store.ListMetaRegistryResourceAsOf(ctx, &store.FindMetaRegistryResourceMessage{
		GUIDPrefix: &guid,
		ObjectType: &tableType,
	}, asOfTime)
	if err != nil {
		return nil, err
	}

	for _, tbl := range tables {
		tableMeta := tbl.Metadata.GetTableMetadata()
		if tableMeta == nil {
			continue
		}
		result.Tables = append(result.Tables, tableMeta)
	}

	// Fetch views
	viewType := storepb.MetaType_VIEW
	views, err := s.store.ListMetaRegistryResourceAsOf(ctx, &store.FindMetaRegistryResourceMessage{
		GUIDPrefix: &guid,
		ObjectType: &viewType,
	}, asOfTime)
	if err != nil {
		return nil, err
	}
	for _, v := range views {
		if vm := v.Metadata.GetViewMetadata(); vm != nil {
			result.Views = append(result.Views, vm)
		}
	}

	// Fetch functions
	funcType := storepb.MetaType_FUNCTION
	funcs, err := s.store.ListMetaRegistryResourceAsOf(ctx, &store.FindMetaRegistryResourceMessage{
		GUIDPrefix: &guid,
		ObjectType: &funcType,
	}, asOfTime)
	if err != nil {
		return nil, err
	}
	for _, f := range funcs {
		if fm := f.Metadata.GetFunctionMetadata(); fm != nil {
			result.Functions = append(result.Functions, fm)
		}
	}

	// Fetch procedures
	procType := storepb.MetaType_PROCEDURE
	procs, err := s.store.ListMetaRegistryResourceAsOf(ctx, &store.FindMetaRegistryResourceMessage{
		GUIDPrefix: &guid,
		ObjectType: &procType,
	}, asOfTime)
	if err != nil {
		return nil, err
	}
	for _, p := range procs {
		if pm := p.Metadata.GetProcedureMetadata(); pm != nil {
			result.Procedures = append(result.Procedures, pm)
		}
	}

	return result, nil
}

func (s *DatabaseService) rebuildSchemaContents(ctx context.Context, schemaGUID string, schemaMeta *storepb.SchemaMetadata, asOfTime time.Time) *storepb.SchemaMetadata {
	result, err := s.rebuildDatabaseObjects(ctx, schemaGUID, asOfTime)
	if err != nil {
		slog.Warn("failed to rebuild schema contents", "schema", schemaGUID, "error", err)
		return schemaMeta // fall back to the stored metadata
	}
	result.Name = schemaMeta.Name
	return result
}

func buildDiffSummary(diff *schema.MetadataDiff) string {
	parts := make([]string, 0)

	countCreate := 0
	countDrop := 0
	countAlter := 0
	for _, tc := range diff.TableChanges {
		switch tc.Action {
		case schema.MetadataDiffActionCreate:
			countCreate++
		case schema.MetadataDiffActionDrop:
			countDrop++
		case schema.MetadataDiffActionAlter:
			countAlter++
		default:
		}
	}
	if countCreate+countDrop+countAlter > 0 {
		parts = append(parts, fmt.Sprintf("Tables: +%d created, ~%d modified, -%d dropped", countCreate, countAlter, countDrop))
	}

	viewCreate := 0
	viewDrop := 0
	for _, vc := range diff.ViewChanges {
		switch vc.Action {
		case schema.MetadataDiffActionCreate:
			viewCreate++
		case schema.MetadataDiffActionDrop:
			viewDrop++
		default:
		}
	}
	if viewCreate+viewDrop > 0 {
		parts = append(parts, fmt.Sprintf("Views: +%d created, -%d dropped", viewCreate, viewDrop))
	}

	funcCreate := 0
	funcDrop := 0
	for _, fc := range diff.FunctionChanges {
		switch fc.Action {
		case schema.MetadataDiffActionCreate:
			funcCreate++
		case schema.MetadataDiffActionDrop:
			funcDrop++
		default:
		}
	}
	if funcCreate+funcDrop > 0 {
		parts = append(parts, fmt.Sprintf("Functions: +%d created, -%d dropped", funcCreate, funcDrop))
	}

	schemaCreate := 0
	schemaDrop := 0
	for _, sc := range diff.SchemaChanges {
		switch sc.Action {
		case schema.MetadataDiffActionCreate:
			schemaCreate++
		case schema.MetadataDiffActionDrop:
			schemaDrop++
		default:
		}
	}
	if schemaCreate+schemaDrop > 0 {
		parts = append(parts, fmt.Sprintf("Schemas: +%d created, -%d dropped", schemaCreate, schemaDrop))
	}

	if len(parts) == 0 {
		return "No changes detected."
	}
	return strings.Join(parts, "\n")
}
