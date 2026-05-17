package v1

import (
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func convertStoredMetadataMessage(meta *storepb.StoredMetadata) *v1pb.StoredMetadata {
	if meta == nil {
		return nil
	}

	result := &v1pb.StoredMetadata{}
	switch v := meta.GetType().(type) {
	case *storepb.StoredMetadata_DatabaseSchemaMetadata:
		result.Type = &v1pb.StoredMetadata_DatabaseSchemaMetadata{
			DatabaseSchemaMetadata: convertDatabaseSchemaMetadata(v.DatabaseSchemaMetadata),
		}
	case *storepb.StoredMetadata_SchemaMetadata:
		result.Type = &v1pb.StoredMetadata_SchemaMetadata{
			SchemaMetadata: convertSchemaMetadata(v.SchemaMetadata),
		}
	case *storepb.StoredMetadata_TableMetadata:
		result.Type = &v1pb.StoredMetadata_TableMetadata{
			TableMetadata: convertTableMetadata(v.TableMetadata),
		}
	case *storepb.StoredMetadata_ExternalTableMetadata:
		result.Type = &v1pb.StoredMetadata_ExternalTableMetadata{
			ExternalTableMetadata: convertExternalTableMetadata(v.ExternalTableMetadata),
		}
	case *storepb.StoredMetadata_ViewMetadata:
		result.Type = &v1pb.StoredMetadata_ViewMetadata{
			ViewMetadata: convertViewMetadata(v.ViewMetadata),
		}
	case *storepb.StoredMetadata_MaterializedViewMetadata:
		result.Type = &v1pb.StoredMetadata_MaterializedViewMetadata{
			MaterializedViewMetadata: convertMaterializedViewMetadata(v.MaterializedViewMetadata),
		}
	case *storepb.StoredMetadata_FunctionMetadata:
		result.Type = &v1pb.StoredMetadata_FunctionMetadata{
			FunctionMetadata: convertFunctionMetadata(v.FunctionMetadata),
		}
	case *storepb.StoredMetadata_ProcedureMetadata:
		result.Type = &v1pb.StoredMetadata_ProcedureMetadata{
			ProcedureMetadata: convertProcedureMetadata(v.ProcedureMetadata),
		}
	case *storepb.StoredMetadata_PackageMetadata:
		result.Type = &v1pb.StoredMetadata_PackageMetadata{
			PackageMetadata: convertPackageMetadata(v.PackageMetadata),
		}
	case *storepb.StoredMetadata_SequenceMetadata:
		result.Type = &v1pb.StoredMetadata_SequenceMetadata{
			SequenceMetadata: convertSequenceMetadata(v.SequenceMetadata),
		}
	case *storepb.StoredMetadata_StreamMetadata:
		result.Type = &v1pb.StoredMetadata_StreamMetadata{
			StreamMetadata: convertStreamMetadata(v.StreamMetadata),
		}
	case *storepb.StoredMetadata_TaskMetadata:
		result.Type = &v1pb.StoredMetadata_TaskMetadata{
			TaskMetadata: convertTaskMetadata(v.TaskMetadata),
		}
	case *storepb.StoredMetadata_ManualSqlMetadata:
		result.Type = &v1pb.StoredMetadata_ManualSqlMetadata{
			ManualSqlMetadata: convertManualSQLMetadata(v.ManualSqlMetadata),
		}
	case *storepb.StoredMetadata_ColumnMetadata:
		result.Type = &v1pb.StoredMetadata_ColumnMetadata{
			ColumnMetadata: convertColumnMetadata(v.ColumnMetadata),
		}
	default:
	}
	return result
}

func convertDatabaseSchemaMetadata(meta *storepb.DatabaseSchemaMetadata) *v1pb.DatabaseSchemaMetadata {
	if meta == nil {
		return nil
	}
	result := &v1pb.DatabaseSchemaMetadata{
		Name:         meta.Name,
		CharacterSet: meta.CharacterSet,
		Collation:    meta.Collation,
		Datashare:    meta.Datashare,
		ServiceName:  meta.ServiceName,
		Owner:        meta.Owner,
		SearchPath:   meta.SearchPath,
	}
	for _, schema := range meta.Schemas {
		result.Schemas = append(result.Schemas, convertSchemaMetadata(schema))
	}
	for _, ext := range meta.Extensions {
		result.Extensions = append(result.Extensions, convertExtensionMetadata(ext))
	}
	for _, db := range meta.LinkedDatabases {
		result.LinkedDatabases = append(result.LinkedDatabases, convertLinkedDatabaseMetadata(db))
	}
	for _, trigger := range meta.EventTriggers {
		result.EventTriggers = append(result.EventTriggers, convertEventTriggerMetadata(trigger))
	}
	return result
}

func convertSchemaMetadata(meta *storepb.SchemaMetadata) *v1pb.SchemaMetadata {
	if meta == nil {
		return nil
	}
	result := &v1pb.SchemaMetadata{
		Name:     meta.Name,
		Owner:    meta.Owner,
		Comment:  meta.Comment,
		SkipDump: meta.SkipDump,
	}
	for _, table := range meta.Tables {
		result.Tables = append(result.Tables, convertTableMetadata(table))
	}
	for _, extTable := range meta.ExternalTables {
		result.ExternalTables = append(result.ExternalTables, convertExternalTableMetadata(extTable))
	}
	for _, view := range meta.Views {
		result.Views = append(result.Views, convertViewMetadata(view))
	}
	for _, fn := range meta.Functions {
		result.Functions = append(result.Functions, convertFunctionMetadata(fn))
	}
	for _, proc := range meta.Procedures {
		result.Procedures = append(result.Procedures, convertProcedureMetadata(proc))
	}
	for _, stream := range meta.Streams {
		result.Streams = append(result.Streams, convertStreamMetadata(stream))
	}
	for _, task := range meta.Tasks {
		result.Tasks = append(result.Tasks, convertTaskMetadata(task))
	}
	for _, mv := range meta.MaterializedViews {
		result.MaterializedViews = append(result.MaterializedViews, convertMaterializedViewMetadata(mv))
	}
	for _, seq := range meta.Sequences {
		result.Sequences = append(result.Sequences, convertSequenceMetadata(seq))
	}
	for _, pkg := range meta.Packages {
		result.Packages = append(result.Packages, convertPackageMetadata(pkg))
	}
	for _, event := range meta.Events {
		result.Events = append(result.Events, convertEventMetadata(event))
	}
	for _, enumType := range meta.EnumTypes {
		result.EnumTypes = append(result.EnumTypes, convertEnumTypeMetadata(enumType))
	}
	return result
}

func convertTableMetadata(meta *storepb.TableMetadata) *v1pb.TableMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.TableMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertExternalTableMetadata(meta *storepb.ExternalTableMetadata) *v1pb.ExternalTableMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.ExternalTableMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertViewMetadata(meta *storepb.ViewMetadata) *v1pb.ViewMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.ViewMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertMaterializedViewMetadata(meta *storepb.MaterializedViewMetadata) *v1pb.MaterializedViewMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.MaterializedViewMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertFunctionMetadata(meta *storepb.FunctionMetadata) *v1pb.FunctionMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.FunctionMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertProcedureMetadata(meta *storepb.ProcedureMetadata) *v1pb.ProcedureMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.ProcedureMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertPackageMetadata(meta *storepb.PackageMetadata) *v1pb.PackageMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.PackageMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertSequenceMetadata(meta *storepb.SequenceMetadata) *v1pb.SequenceMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.SequenceMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertStreamMetadata(meta *storepb.StreamMetadata) *v1pb.StreamMetadata {
	if meta == nil {
		return nil
	}
	result := &v1pb.StreamMetadata{
		Name:       meta.Name,
		TableName:  meta.TableName,
		Owner:      meta.Owner,
		Comment:    meta.Comment,
		Type:       v1pb.StreamMetadata_Type(meta.Type),
		Stale:      meta.Stale,
		Mode:       v1pb.StreamMetadata_Mode(meta.Mode),
		Definition: meta.Definition,
	}
	return result
}

func convertTaskMetadata(meta *storepb.TaskMetadata) *v1pb.TaskMetadata {
	if meta == nil {
		return nil
	}
	result := &v1pb.TaskMetadata{
		Name:         meta.Name,
		Id:           meta.Id,
		Owner:        meta.Owner,
		Comment:      meta.Comment,
		Warehouse:    meta.Warehouse,
		Schedule:     meta.Schedule,
		Predecessors: meta.Predecessors,
		State:        v1pb.TaskMetadata_State(meta.State),
		Condition:    meta.Condition,
		Definition:   meta.Definition,
	}
	return result
}

func convertExtensionMetadata(meta *storepb.ExtensionMetadata) *v1pb.ExtensionMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.ExtensionMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertLinkedDatabaseMetadata(meta *storepb.LinkedDatabaseMetadata) *v1pb.LinkedDatabaseMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.LinkedDatabaseMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertEventTriggerMetadata(meta *storepb.EventTriggerMetadata) *v1pb.EventTriggerMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.EventTriggerMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertEventMetadata(meta *storepb.EventMetadata) *v1pb.EventMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.EventMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertEnumTypeMetadata(meta *storepb.EnumTypeMetadata) *v1pb.EnumTypeMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.EnumTypeMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}

func convertManualSQLMetadata(meta *storepb.ManualSQLMetadata) *v1pb.ManualSQLMetadata {
	if meta == nil {
		return nil
	}
	result := &v1pb.ManualSQLMetadata{
		ManualSqlId:      meta.ManualSqlId,
		Name:             meta.Name,
		Title:            meta.Title,
		Comment:          meta.Comment,
		SqlText:          meta.SqlText,
		Tags:             meta.Tags,
		Attributes:       meta.Attributes,
		SchemaName:       meta.SchemaName,
		InstanceResource: meta.InstanceResource,
		DatabaseName:     meta.DatabaseName,
	}
	return result
}

func convertColumnMetadata(meta *storepb.ColumnMetadata) *v1pb.ColumnMetadata {
	if meta == nil {
		return nil
	}
	data, _ := proto.Marshal(meta)
	result := &v1pb.ColumnMetadata{}
	_ = proto.Unmarshal(data, result)
	return result
}
