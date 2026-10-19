package dataquality

import (
	"context"

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

	columns, err := metadataColumns(resource.Metadata, metaType)
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
		MetaHash:           resource.MetaHash,
	}
	return &Resolution{Target: target, Instance: instance, Database: database, Metadata: resource.Metadata}, nil
}

// metadataColumns returns the columns a registered relation exposes, classified
// for rule validation. A relation type without a column list is a target a rule
// cannot watch with a column-level check; it resolves with no columns so a
// table-level rule still compiles.
func metadataColumns(metadata *storepb.StoredMetadata, metaType storepb.MetaType) ([]ColumnInfo, error) {
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
			Kind:     ClassifyColumnType(column.GetType()),
			Nullable: column.GetNullable(),
		})
	}
	return columns, nil
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
