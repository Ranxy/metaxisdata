package dataquality

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// ErrOrphaned reports that a rule's target no longer resolves: the registry row is
// gone, the instance was deleted, or the database was removed. The runner records
// it as SKIPPED(ORPHANED) rather than ERROR.
var ErrOrphaned = errors.New("the rule target no longer exists")

// orphaned builds the NotFound error for a target that no longer resolves.
func orphaned(format string, args ...any) error {
	return &common.Error{Code: common.NotFound, Err: errors.Wrapf(ErrOrphaned, format, args...)}
}

// internal builds an Internal error that still wraps its cause.
func internal(err error, format string, args ...any) error {
	return &common.Error{Code: common.Internal, Err: errors.Wrapf(err, format, args...)}
}

// Resolution is the metadata a rule validates against: the registry row, the
// instance and database it lives in, and the columns its engine reports.
type Resolution struct {
	Target ResolvedTarget
	// Instance is the target's instance row, with its data sources and engine.
	Instance *store.InstanceMessage
	// Database is the target's database row.
	Database *store.DatabaseMessage
	// Metadata is the stored metadata of the target object, for DDL and column
	// comments.
	Metadata *storepb.StoredMetadata
}

// ResolveTarget loads the metadata a rule needs for one target GUID.
//
// A target that does not exist is a NotFound error (a create must be refused); the
// error wraps ErrOrphaned so a caller that only needs to know "gone" can test it.
func ResolveTarget(ctx context.Context, s *store.Store, guid string, metaType storepb.MetaType) (*Resolution, error) {
	if s == nil {
		return nil, common.Errorf(common.Internal, "a store is required to resolve a target")
	}
	if guid == "" {
		return nil, invalidConfig("the target GUID is required")
	}
	if metaType == storepb.MetaType_UNSPECIFIED {
		return nil, invalidConfig("the target object type is required")
	}
	resource, err := s.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
		GUID:       &guid,
		ObjectType: &metaType,
	})
	if err != nil {
		return nil, internal(err, "failed to read the target metadata")
	}
	if resource == nil {
		return nil, orphaned("target %q (%s) is not in the metadata registry", guid, metaType)
	}

	segments := common.SplitMetaGUID(guid)
	if len(segments) < 3 {
		return nil, orphaned("target GUID %q does not name an object", guid)
	}
	instanceResourceID := segments[0]
	databaseName := segments[1]
	objectName := segments[len(segments)-1]
	schemaName := ""
	if len(segments) == 4 {
		schemaName = segments[2]
	}

	instance, err := s.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceResourceID})
	if err != nil {
		return nil, internal(err, "failed to read the target instance")
	}
	if instance == nil || instance.Deleted {
		return nil, orphaned("the target instance %q is gone", instanceResourceID)
	}
	if instance.Metadata == nil {
		return nil, orphaned("the target instance %q has no metadata", instanceResourceID)
	}

	database, err := s.GetDatabase(ctx, &store.FindDatabaseMessage{
		InstanceID:   &instanceResourceID,
		DatabaseName: &databaseName,
	})
	if err != nil {
		return nil, internal(err, "failed to read the target database")
	}
	if database == nil || database.Deleted {
		return nil, orphaned("the target database %q is gone", databaseName)
	}

	columns, err := metadataColumns(resource.Metadata, metaType, instance.Metadata.GetEngine())
	if err != nil {
		return nil, err
	}

	path := &storepb.QualityTargetPath{
		Instance: instanceResourceID,
		Database: databaseName,
		Schema:   schemaName,
		Object:   objectName,
	}
	target := ResolvedTarget{
		Target: Target{
			GUID:     guid,
			MetaType: metaType,
			Path:     path,
		},
		Engine:             instance.Metadata.GetEngine(),
		InstanceResourceID: instanceResourceID,
		Database:           databaseName,
		Schema:             schemaName,
		Table:              objectName,
		Columns:            columns,
		KeyColumns:         primaryKeyColumns(resource.Metadata, metaType, columns),
		MetaHash:           resource.MetaHash,
	}
	return &Resolution{Target: target, Instance: instance, Database: database, Metadata: resource.Metadata}, nil
}

// metadataColumns returns the columns a registered relation exposes, classified
// for rule validation. A relation type without a column list is a target a rule
// cannot watch with a column-level check; it resolves with no columns so a
// table-level rule still compiles.
func metadataColumns(metadata *storepb.StoredMetadata, metaType storepb.MetaType, engine storepb.Engine) ([]ColumnInfo, error) {
	var stored []*storepb.ColumnMetadata
	switch metaType {
	case storepb.MetaType_TABLE:
		stored = metadata.GetTableMetadata().GetColumns()
	case storepb.MetaType_EXTERNAL_TABLE:
		stored = metadata.GetExternalTableMetadata().GetColumns()
	case storepb.MetaType_VIEW:
		stored = metadata.GetViewMetadata().GetColumns()
	case storepb.MetaType_MATERIALIZED_VIEW:
		stored = metadata.GetMaterializedViewMetadata().GetColumns()
	default:
		return nil, invalidConfig("a rule target must be a table or view, not %s", metaType)
	}
	columns := make([]ColumnInfo, 0, len(stored))
	for _, column := range stored {
		if column == nil {
			continue
		}
		columns = append(columns, ColumnInfo{
			Name:     column.GetName(),
			Type:     column.GetType(),
			Kind:     classifyColumnTypeForEngine(engine, column.GetType()),
			Nullable: column.GetNullable(),
		})
	}
	return columns, nil
}

// primaryKeyColumns returns the primary-key columns of a registered relation in
// key order, spelled the way the registry spells its columns. Only an index key
// that names a plain column counts: a functional key has no column to select.
// The failure sample uses the key to identify a sampled row; a relation whose
// sync recorded no primary key returns nil, and the sample keeps the checked
// columns only.
func primaryKeyColumns(metadata *storepb.StoredMetadata, metaType storepb.MetaType, columns []ColumnInfo) []string {
	var indexes []*storepb.IndexMetadata
	switch metaType {
	case storepb.MetaType_TABLE:
		indexes = metadata.GetTableMetadata().GetIndexes()
	case storepb.MetaType_EXTERNAL_TABLE:
		// A foreign table's sync records no indexes, so it has no key to add.
		return nil
	case storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW:
		// A view is restricted to table-level rules; the one that samples (custom
		// SQL) returns the rows of its own statement, so it has no key of ours.
		return nil
	default:
		return nil
	}
	for _, index := range indexes {
		if index == nil || !index.GetPrimary() {
			continue
		}
		keys := make([]string, 0, len(index.GetExpressions()))
		for _, expression := range index.GetExpressions() {
			name, ok := indexColumnName(expression)
			if !ok {
				continue
			}
			for _, column := range columns {
				if column.Name == name {
					keys = append(keys, column.Name)
					break
				}
			}
		}
		if len(keys) > 0 {
			return keys
		}
		return nil
	}
	return nil
}

// indexColumnName reduces one index key expression to the column it names. The
// engines print a key differently: PostgreSQL prints it the way it must be
// written, so a mixed-case or reserved word arrives quoted ("TeamID", "order")
// and a descending key gets a DESC suffix; MySQL and SQL Server record the bare
// catalog name, so accepting backticks and brackets there is defensive. A
// functional key such as lower(email) or a key with a non-default collation or
// operator class names no single column and is skipped rather than guessed at.
func indexColumnName(expression string) (string, bool) {
	name := strings.TrimSpace(expression)
	for _, suffix := range []string{" desc", " asc"} {
		if len(name) >= len(suffix) && strings.EqualFold(name[len(name)-len(suffix):], suffix) {
			name = strings.TrimSpace(name[:len(name)-len(suffix)])
			break
		}
	}
	if len(name) >= 2 {
		switch {
		case name[0] == '"' && name[len(name)-1] == '"':
			return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`), true
		case name[0] == '`' && name[len(name)-1] == '`':
			return strings.ReplaceAll(name[1:len(name)-1], "``", "`"), true
		case name[0] == '[' && name[len(name)-1] == ']':
			return strings.ReplaceAll(name[1:len(name)-1], "]]", "]"), true
		}
	}
	if name == "" || strings.ContainsAny(name, " \t()\"'`[]") {
		return "", false
	}
	return name, true
}

// RequireTarget verifies the rule type may watch this object type and engine.
// Views have no per-column type authority, so only table-level checks are
// accepted there.
func RequireTarget(resolution *Resolution, ruleType storepb.QualityRuleType) error {
	if resolution == nil {
		return common.Errorf(common.Internal, "a resolved target is required")
	}
	if !RuleTypeForTarget(resolution.Target.MetaType, ruleType) {
		return invalidConfig("rule type %s cannot watch a %s", ruleType, resolution.Target.MetaType)
	}
	if ok, reason := Supported(resolution.Target.Engine, ruleType); !ok {
		return invalidConfig("%s", reason)
	}
	return nil
}
