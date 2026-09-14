package v1

import (
	"context"
	"fmt"
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
	instanceGUID, ok := common.GetInstanceFromGUID(req.Msg.Guid)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid guid %q", req.Msg.Guid))
	}

	schemaName, ok := common.GetSchemaFromGUID(req.Msg.Guid)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid guid %q", req.Msg.Guid))
	}

	// Prefer the engine's own DDL captured at sync time. It is exact where the
	// metadata -> DDL reconstruction is lossy (StarRocks distribution,
	// properties, partitioning), and it is written only for engines that can
	// produce one. A miss is the normal state for other engines, so it falls
	// through to the reconstruction path below.
	if ddl, found, err := s.store.GetMetaRegistrySchema(ctx, req.Msg.Guid, storepb.MetaType(req.Msg.MetaType)); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get object definition %q: %v", req.Msg.Guid, err))
	} else if found {
		return connect.NewResponse(&v1pb.MetadataSchemaString{Schema: ddl}), nil
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
	instanceGUID, ok := common.GetInstanceFromGUID(guid)
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
	sourceTime := req.Msg.GetSourceTime()
	if sourceTime == nil {
		// The proto promises "the earliest available version"; defaulting to now
		// made source and target identical, so the diff was always empty.
		earliest, err := s.earliestVersionTime(ctx, guid)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to find the earliest version"))
		}
		if !earliest.IsZero() {
			sourceTime = timestamppb.New(earliest)
		}
	}
	sourceMeta, err := s.buildDatabaseSchemaAtTime(ctx, guid, sourceTime)
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

// earliestVersionTime returns the valid_from of the oldest history row in the
// GUID subtree. That is the "earliest available version" the diff falls back to
// when source_time is not set.
func (s *DatabaseService) earliestVersionTime(ctx context.Context, guid string) (time.Time, error) {
	limit := 1
	history, err := s.store.ListMetaRegistryHistory(ctx, &store.FindMetaRegistryHistoryMessage{
		GUIDPrefix: &guid,
		Limit:      &limit,
	})
	if err != nil {
		return time.Time{}, err
	}
	if len(history) == 0 {
		return time.Time{}, nil
	}
	return history[0].ValidFrom, nil
}

func (s *DatabaseService) buildDatabaseSchemaAtTime(ctx context.Context, guid string, asOf *timestamppb.Timestamp) (*storepb.DatabaseSchemaMetadata, error) {
	var asOfTime time.Time
	if asOf != nil {
		asOfTime = asOf.AsTime()
	} else {
		asOfTime = time.Now()
	}

	// Determine the scope: database-level or schema-level
	parts := common.SplitMetaGUID(guid)
	if len(parts) < 2 {
		return nil, errors.Errorf("guid %q is not deep enough (need at least instance;database)", guid)
	}

	result := &storepb.DatabaseSchemaMetadata{
		Name: parts[1], // database name
	}

	// Database-level attributes (charset, collation, extensions, event triggers,
	// search path) live on the DATABASE registry row rather than on schema rows,
	// so they have to be restored from that version too.
	databaseObject, err := s.store.GetMetaRegistryAsOf(ctx, &store.FindMetaRegistryResourceMessage{
		GUID:       &guid,
		ObjectType: storepb.MetaType_DATABASE.Enum(),
	}, asOfTime)
	if err != nil {
		return nil, err
	}
	if databaseObject != nil {
		if dbMeta := databaseObject.Metadata.GetDatabaseSchemaMetadata(); dbMeta != nil {
			if dbMeta.Name != "" {
				result.Name = dbMeta.Name
			}
			result.CharacterSet = dbMeta.CharacterSet
			result.Collation = dbMeta.Collation
			result.Extensions = dbMeta.Extensions
			result.Datashare = dbMeta.Datashare
			result.Owner = dbMeta.Owner
			result.SearchPath = dbMeta.SearchPath
			result.EventTriggers = dbMeta.EventTriggers
		}
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
		rebuilt, err := s.rebuildSchemaContents(ctx, schemaGUID, schemaMeta, asOfTime)
		if err != nil {
			// Falling back to the current metadata produced a plausible but
			// wrong historical diff, so the error propagates instead.
			return nil, err
		}
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

	// Every schema-level object type the registry stores has to be rebuilt, or a
	// change to it is reported as "no changes detected". Each entry maps a meta
	// type onto the SchemaMetadata slice it fills.
	type objectType struct {
		metaType storepb.MetaType
		collect  func(result *storepb.SchemaMetadata, meta *storepb.StoredMetadata) bool
	}
	objectTypes := []objectType{
		{storepb.MetaType_TABLE, func(r *storepb.SchemaMetadata, meta *storepb.StoredMetadata) bool {
			if v := meta.GetTableMetadata(); v != nil {
				r.Tables = append(r.Tables, v)
				return true
			}
			return false
		}},
		{storepb.MetaType_EXTERNAL_TABLE, func(r *storepb.SchemaMetadata, meta *storepb.StoredMetadata) bool {
			if v := meta.GetExternalTableMetadata(); v != nil {
				r.ExternalTables = append(r.ExternalTables, v)
				return true
			}
			return false
		}},
		{storepb.MetaType_VIEW, func(r *storepb.SchemaMetadata, meta *storepb.StoredMetadata) bool {
			if v := meta.GetViewMetadata(); v != nil {
				r.Views = append(r.Views, v)
				return true
			}
			return false
		}},
		{storepb.MetaType_MATERIALIZED_VIEW, func(r *storepb.SchemaMetadata, meta *storepb.StoredMetadata) bool {
			if v := meta.GetMaterializedViewMetadata(); v != nil {
				r.MaterializedViews = append(r.MaterializedViews, v)
				return true
			}
			return false
		}},
		{storepb.MetaType_FUNCTION, func(r *storepb.SchemaMetadata, meta *storepb.StoredMetadata) bool {
			if v := meta.GetFunctionMetadata(); v != nil {
				r.Functions = append(r.Functions, v)
				return true
			}
			return false
		}},
		{storepb.MetaType_PROCEDURE, func(r *storepb.SchemaMetadata, meta *storepb.StoredMetadata) bool {
			if v := meta.GetProcedureMetadata(); v != nil {
				r.Procedures = append(r.Procedures, v)
				return true
			}
			return false
		}},
		{storepb.MetaType_SEQUENCE, func(r *storepb.SchemaMetadata, meta *storepb.StoredMetadata) bool {
			if v := meta.GetSequenceMetadata(); v != nil {
				r.Sequences = append(r.Sequences, v)
				return true
			}
			return false
		}},
	}

	for _, objectType := range objectTypes {
		metaType := objectType.metaType
		objects, err := s.store.ListMetaRegistryResourceAsOf(ctx, &store.FindMetaRegistryResourceMessage{
			GUIDPrefix: &guid,
			ObjectType: &metaType,
		}, asOfTime)
		if err != nil {
			return nil, err
		}
		for _, object := range objects {
			objectType.collect(result, object.Metadata)
		}
	}

	return result, nil
}

func (s *DatabaseService) rebuildSchemaContents(ctx context.Context, schemaGUID string, schemaMeta *storepb.SchemaMetadata, asOfTime time.Time) (*storepb.SchemaMetadata, error) {
	result, err := s.rebuildDatabaseObjects(ctx, schemaGUID, asOfTime)
	if err != nil {
		return nil, err
	}
	// The schema's own attributes are not separate objects in the registry, so
	// they come from the version being reconstructed. Enum types and events live
	// only inside SchemaMetadata (there is no meta type for them), so dropping
	// them here made their changes diff as "no changes detected".
	result.Name = schemaMeta.Name
	result.Owner = schemaMeta.Owner
	result.Comment = schemaMeta.Comment
	result.SkipDump = schemaMeta.SkipDump
	result.Events = schemaMeta.Events
	result.EnumTypes = schemaMeta.EnumTypes
	return result, nil
}

// countDiffActions tallies create/alter/drop for one category of the diff.
func countDiffActions[T any](changes []T, action func(T) schema.MetadataDiffAction) (created, altered, dropped int) {
	for _, change := range changes {
		switch action(change) {
		case schema.MetadataDiffActionCreate:
			created++
		case schema.MetadataDiffActionAlter:
			altered++
		case schema.MetadataDiffActionDrop:
			dropped++
		default:
		}
	}
	return created, altered, dropped
}

func buildDiffSummary(diff *schema.MetadataDiff) string {
	// Every category the differ reports has to appear here: the summary used to
	// count only tables, views, functions and schemas, so a change to a
	// materialized view, sequence, enum type or event was summarised as
	// "No changes detected." even though the DDL contained it.
	schemaC, schemaA, schemaD := countDiffActions(diff.SchemaChanges, func(c *schema.SchemaDiff) schema.MetadataDiffAction { return c.Action })
	tableC, tableA, tableD := countDiffActions(diff.TableChanges, func(c *schema.TableDiff) schema.MetadataDiffAction { return c.Action })
	viewC, viewA, viewD := countDiffActions(diff.ViewChanges, func(c *schema.ViewDiff) schema.MetadataDiffAction { return c.Action })
	mvC, mvA, mvD := countDiffActions(diff.MaterializedViewChanges, func(c *schema.MaterializedViewDiff) schema.MetadataDiffAction { return c.Action })
	funcC, funcA, funcD := countDiffActions(diff.FunctionChanges, func(c *schema.FunctionDiff) schema.MetadataDiffAction { return c.Action })
	procC, procA, procD := countDiffActions(diff.ProcedureChanges, func(c *schema.ProcedureDiff) schema.MetadataDiffAction { return c.Action })
	seqC, seqA, seqD := countDiffActions(diff.SequenceChanges, func(c *schema.SequenceDiff) schema.MetadataDiffAction { return c.Action })
	enumC, enumA, enumD := countDiffActions(diff.EnumTypeChanges, func(c *schema.EnumTypeDiff) schema.MetadataDiffAction { return c.Action })
	extC, extA, extD := countDiffActions(diff.ExtensionChanges, func(c *schema.ExtensionDiff) schema.MetadataDiffAction { return c.Action })
	triggerC, triggerA, triggerD := countDiffActions(diff.EventTriggerChanges, func(c *schema.EventTriggerDiff) schema.MetadataDiffAction { return c.Action })
	eventC, eventA, eventD := countDiffActions(diff.EventChanges, func(c *schema.EventDiff) schema.MetadataDiffAction { return c.Action })
	commentC, commentA, commentD := countDiffActions(diff.CommentChanges, func(c *schema.CommentDiff) schema.MetadataDiffAction { return c.Action })

	categories := []struct {
		label                     string
		created, altered, dropped int
	}{
		{"Schemas", schemaC, schemaA, schemaD},
		{"Tables", tableC, tableA, tableD},
		{"Views", viewC, viewA, viewD},
		{"Materialized views", mvC, mvA, mvD},
		{"Functions", funcC, funcA, funcD},
		{"Procedures", procC, procA, procD},
		{"Sequences", seqC, seqA, seqD},
		{"Enum types", enumC, enumA, enumD},
		{"Extensions", extC, extA, extD},
		{"Event triggers", triggerC, triggerA, triggerD},
		{"Events", eventC, eventA, eventD},
		{"Comments", commentC, commentA, commentD},
	}

	parts := make([]string, 0, len(categories))
	for _, category := range categories {
		if category.created+category.altered+category.dropped == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: +%d created, ~%d modified, -%d dropped",
			category.label, category.created, category.altered, category.dropped))
	}

	if len(parts) == 0 {
		return "No changes detected."
	}
	return strings.Join(parts, "\n")
}
