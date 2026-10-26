package store

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityRuleColumns is the column list every quality_rule read returns, in the
// order scanQualityRule consumes it.
const qualityRuleColumns = `id, resource_id, title, description, dimension, rule_type,
	target_guid, target_type, target_path, target_meta_hash, config, severity, state,
	owner_id, tags, schedule_interval_seconds, next_run_at, mute_until, revision,
	created_by, updated_by, created_at, updated_at, deleted`

// qualityRuleRevisionColumns is the column list every quality_rule_revision read
// returns, in the order scanQualityRuleRevision consumes it.
const qualityRuleRevisionColumns = `id, rule_id, revision, target_guid, target_type,
	target_path, target_meta_hash, dimension, rule_type, severity, config, created_by,
	created_at`

// QualityRuleMessage is the store representation of a row in quality_rule, the
// mutable head of a data-quality rule. The immutable definition history lives in
// quality_rule_revision and is written from the same transaction on every
// semantic edit.
type QualityRuleMessage struct {
	ID             int64
	ResourceID     string
	Title          string
	Description    string
	Dimension      storepb.QualityDimension
	RuleType       storepb.QualityRuleType
	TargetGUID     string
	TargetType     storepb.MetaType
	TargetPath     *storepb.QualityTargetPath
	TargetMetaHash []byte
	Config         *storepb.QualityRuleConfig
	Severity       storepb.QualitySeverity
	State          storepb.QualityRuleState
	// OwnerID is the principal id of the rule's owner; nil means unassigned.
	OwnerID *int32
	Tags    []string
	// ScheduleInterval is how often a scheduled run considers the rule. The
	// column stores whole seconds.
	ScheduleInterval time.Duration
	// NextRunAt is when the scheduler next considers the rule due; nil means the
	// rule is not scheduled.
	NextRunAt *time.Time
	// MuteUntil suppresses rule-level issue notifications until it passes. The
	// rule is still executed and its results are still recorded.
	MuteUntil *time.Time
	Revision  int32
	CreatedBy *int32
	UpdatedBy *int32
	CreatedAt time.Time
	UpdatedAt time.Time
	Deleted   bool
}

// QualityRuleRevisionMessage is one immutable snapshot from
// quality_rule_revision. A result records the revision that produced it, so an
// edit never rewrites the definition behind a historical result.
type QualityRuleRevisionMessage struct {
	ID             int64
	RuleID         int64
	Revision       int32
	TargetGUID     string
	TargetType     storepb.MetaType
	TargetPath     *storepb.QualityTargetPath
	TargetMetaHash []byte
	Dimension      storepb.QualityDimension
	RuleType       storepb.QualityRuleType
	Severity       storepb.QualitySeverity
	Config         *storepb.QualityRuleConfig
	CreatedBy      *int32
	CreatedAt      time.Time
}

// FindQualityRuleMessage is the query filter for quality rules.
type FindQualityRuleMessage struct {
	ResourceID  *string
	ResourceIDs []string
	// TargetGUID is a GUID prefix: the rule's target_guid must equal it or live
	// under it. That is what makes a table filter also match its columns.
	TargetGUID *string
	// Instance, Database, Schema and TablePrefix narrow the target by path. They
	// are object names (Instance is the instance resource id, the first GUID
	// segment), not substrings; the deepest one set decides how many segments the
	// subtree condition carries. Schema may be empty for engines without schemas
	// (MySQL), but a deeper filter requires its ancestors.
	Instance    string
	Database    string
	Schema      string
	TablePrefix string
	// The enum filters skip their condition when left UNSPECIFIED.
	Dimension storepb.QualityDimension
	RuleType  storepb.QualityRuleType
	Severity  storepb.QualitySeverity
	State     storepb.QualityRuleState
	OwnerID   *int32
	// OwnerUnassigned selects only rules with no owner (owner_id IS NULL). It is
	// mutually exclusive with OwnerID.
	OwnerUnassigned bool
	// Search matches title or description as a case-insensitive substring.
	Search *string
	// IncludeDeleted keeps soft-deleted rows. The default excludes them.
	IncludeDeleted bool
	// IncludeArchived keeps ARCHIVED rows. The default excludes them; a State
	// filter can only select archived rules together with this flag.
	IncludeArchived bool
	Limit           *int
	Offset          *int
}

// UpdateQualityRuleMessage contains the mutable fields of a quality rule. Only
// non-nil fields change; the nullable ones use the documented sentinel to clear
// their column.
type UpdateQualityRuleMessage struct {
	Title       *string
	Description *string
	// Dimension, RuleType, TargetGUID, TargetType, TargetPath, TargetMetaHash and
	// Config are the rule definition: changing any of them bumps the revision and
	// appends a quality_rule_revision row.
	Dimension      *storepb.QualityDimension
	RuleType       *storepb.QualityRuleType
	TargetGUID     *string
	TargetType     *storepb.MetaType
	TargetPath     *storepb.QualityTargetPath
	TargetMetaHash *[]byte
	Config         *storepb.QualityRuleConfig
	Severity       *storepb.QualitySeverity
	State          *storepb.QualityRuleState
	// OwnerID uses 0 to clear the owner (NULL); principals have ids above 0.
	OwnerID          *int32
	Tags             *[]string
	ScheduleInterval *time.Duration
	// NextRunAt and MuteUntil use a nil or zero time to clear the column to NULL,
	// because the columns are nullable and an edit must be able to unset them.
	NextRunAt *time.Time
	MuteUntil *time.Time
	UpdatedBy *int32
	// ExpectedRevision is an optimistic-concurrency guard: when set and the head's
	// revision differs, the update fails with common.Conflict instead of
	// overwriting a concurrent definition change.
	ExpectedRevision *int32
}

// qualityRuleScanner is the row shape scanned from either a *sql.Row or
// *sql.Rows. It exists so the mapping is testable without a database.
type qualityRuleScanner interface {
	Scan(dest ...any) error
}

// scanQualityRule maps one quality_rule row to the message. The JSONB columns
// are protojson payloads of the messages named in their column comments, and the
// revision/state columns map straight onto the store enums.
func scanQualityRule(row qualityRuleScanner) (*QualityRuleMessage, error) {
	var (
		msg             QualityRuleMessage
		targetPathJSON  []byte
		configJSON      []byte
		ownerID         sql.NullInt32
		tags            pq.StringArray
		intervalSeconds int32
		nextRunAt       sql.NullTime
		muteUntil       sql.NullTime
		createdBy       sql.NullInt32
		updatedBy       sql.NullInt32
	)
	if err := row.Scan(
		&msg.ID,
		&msg.ResourceID,
		&msg.Title,
		&msg.Description,
		&msg.Dimension,
		&msg.RuleType,
		&msg.TargetGUID,
		&msg.TargetType,
		&targetPathJSON,
		&msg.TargetMetaHash,
		&configJSON,
		&msg.Severity,
		&msg.State,
		&ownerID,
		&tags,
		&intervalSeconds,
		&nextRunAt,
		&muteUntil,
		&msg.Revision,
		&createdBy,
		&updatedBy,
		&msg.CreatedAt,
		&msg.UpdatedAt,
		&msg.Deleted,
	); err != nil {
		return nil, err
	}

	msg.TargetPath = &storepb.QualityTargetPath{}
	if err := unmarshalQualityProto(targetPathJSON, msg.TargetPath); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality rule target path")
	}
	msg.Config = &storepb.QualityRuleConfig{}
	if err := unmarshalQualityProto(configJSON, msg.Config); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality rule config")
	}
	msg.OwnerID = qualityRuleInt32(ownerID)
	msg.Tags = []string(tags)
	msg.ScheduleInterval = time.Duration(intervalSeconds) * time.Second
	msg.NextRunAt = qualityRuleTime(nextRunAt)
	msg.MuteUntil = qualityRuleTime(muteUntil)
	msg.CreatedBy = qualityRuleInt32(createdBy)
	msg.UpdatedBy = qualityRuleInt32(updatedBy)
	return &msg, nil
}

// scanQualityRuleRevision maps one quality_rule_revision row to the message.
func scanQualityRuleRevision(row qualityRuleScanner) (*QualityRuleRevisionMessage, error) {
	var (
		msg            QualityRuleRevisionMessage
		targetPathJSON []byte
		configJSON     []byte
		createdBy      sql.NullInt32
	)
	if err := row.Scan(
		&msg.ID,
		&msg.RuleID,
		&msg.Revision,
		&msg.TargetGUID,
		&msg.TargetType,
		&targetPathJSON,
		&msg.TargetMetaHash,
		&msg.Dimension,
		&msg.RuleType,
		&msg.Severity,
		&configJSON,
		&createdBy,
		&msg.CreatedAt,
	); err != nil {
		return nil, err
	}

	msg.TargetPath = &storepb.QualityTargetPath{}
	if err := unmarshalQualityProto(targetPathJSON, msg.TargetPath); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality rule revision target path")
	}
	msg.Config = &storepb.QualityRuleConfig{}
	if err := unmarshalQualityProto(configJSON, msg.Config); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal quality rule revision config")
	}
	msg.CreatedBy = qualityRuleInt32(createdBy)
	return &msg, nil
}

// marshalQualityProto renders a proto message as the JSONB value its column
// stores. A nil message becomes an empty object rather than protojson's "null":
// the JSONB columns are NOT NULL and every reader expects an object.
func marshalQualityProto(m proto.Message) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	raw, err := protojson.Marshal(m)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(raw, []byte("null")) {
		return []byte("{}"), nil
	}
	return raw, nil
}

// unmarshalQualityProto reads a JSONB column into m. Empty and "null"
// payloads leave m at its zero value, so a row written before the object shape
// was enforced still reads.
func unmarshalQualityProto(raw []byte, m proto.Message) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	// The wrapper is project policy: plain protojson.Unmarshal is forbidden.
	return common.ProtojsonUnmarshaler.Unmarshal(raw, m)
}

// qualityRuleInt32 converts a nullable column into the message's optional id.
func qualityRuleInt32(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	n := v.Int32
	return &n
}

// qualityRuleTime converts a nullable timestamp into the message's optional time.
func qualityRuleTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

// qualityRuleWriteTime maps a nullable timestamp patch to the driver argument.
// nil and the zero time both write NULL so a caller can clear next_run_at or
// mute_until through the same pointer field.
func qualityRuleWriteTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return *t
}

// qualityRuleWriteInt32 maps an optional principal id to the driver argument.
// A nil or non-positive id writes NULL: principals have ids above 0.
func qualityRuleWriteInt32(v *int32) any {
	if v == nil || *v <= 0 {
		return nil
	}
	return *v
}

// qualityRuleStateIsValid reports whether state is one of the three states the
// lifecycle allows. UNSPECIFIED is not a state.
func qualityRuleStateIsValid(state storepb.QualityRuleState) bool {
	return state == storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED ||
		state == storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED ||
		state == storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED
}

// metaGUIDPrefix builds the target GUID prefix the instance/database/schema/
// table filters select, or "" when no path filter is set. The deepest filter
// decides the segment count; an intermediate segment may be empty (MySQL has no
// schema segment), but a deeper filter needs its ancestors because a bare name
// is not a GUID prefix. Rules and suggestions filter the same way, so the rule
// applies to both.
func metaGUIDPrefix(instance, database, schema, table string) (string, error) {
	switch {
	case table != "":
		if instance == "" || database == "" {
			return "", common.Errorf(common.Invalid, "a table filter requires instance and database")
		}
		return common.BuildMetaGUID(instance, database, schema, table), nil
	case schema != "":
		if instance == "" || database == "" {
			return "", common.Errorf(common.Invalid, "a schema filter requires instance and database")
		}
		return common.BuildMetaGUID(instance, database, schema), nil
	case database != "":
		if instance == "" {
			return "", common.Errorf(common.Invalid, "a database filter requires instance")
		}
		return common.BuildMetaGUID(instance, database), nil
	case instance != "":
		return common.BuildMetaGUID(instance), nil
	default:
		return "", nil
	}
}

// qualityRuleGUIDPrefix builds the target GUID prefix the rule list path filters
// select.
func qualityRuleGUIDPrefix(find *FindQualityRuleMessage) (string, error) {
	return metaGUIDPrefix(find.Instance, find.Database, find.Schema, find.TablePrefix)
}

// buildQualityRuleWhere renders the filter as a SQL predicate plus its
// parameterized arguments. It is a pure function (no database, no clock) so the
// scoping rules are guard-tested without a live PostgreSQL.
//
// Ordering and the "newest first" listing direction are fixed in
// ListQualityRules, not here: the builder never interpolates caller input.
func buildQualityRuleWhere(find *FindQualityRuleMessage) (string, []any, error) {
	if find == nil {
		find = &FindQualityRuleMessage{}
	}
	where := []string{"TRUE"}
	args := []any{}

	if v := find.ResourceID; v != nil && *v != "" {
		args = append(args, strings.TrimSpace(*v))
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)))
	}
	if len(find.ResourceIDs) > 0 {
		args = append(args, pq.Array(find.ResourceIDs))
		where = append(where, fmt.Sprintf("resource_id = ANY($%d)", len(args)))
	}
	if v := find.TargetGUID; v != nil && *v != "" {
		where, args = appendGUIDSubtreeCondition(where, args, "target_guid", *v)
	}
	prefix, err := qualityRuleGUIDPrefix(find)
	if err != nil {
		return "", nil, err
	}
	if prefix != "" {
		where, args = appendGUIDSubtreeCondition(where, args, "target_guid", prefix)
	}

	if find.Dimension != storepb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED {
		args = append(args, find.Dimension)
		where = append(where, fmt.Sprintf("dimension = $%d", len(args)))
	}
	if find.RuleType != storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
		args = append(args, find.RuleType)
		where = append(where, fmt.Sprintf("rule_type = $%d", len(args)))
	}
	if find.Severity != storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED {
		args = append(args, find.Severity)
		where = append(where, fmt.Sprintf("severity = $%d", len(args)))
	}
	if find.State != storepb.QualityRuleState_QUALITY_RULE_STATE_UNSPECIFIED {
		args = append(args, find.State)
		where = append(where, fmt.Sprintf("state = $%d", len(args)))
	}
	if v := find.OwnerID; v != nil {
		args = append(args, *v)
		where = append(where, fmt.Sprintf("owner_id = $%d", len(args)))
	}
	if find.OwnerUnassigned {
		where = append(where, "owner_id IS NULL")
	}
	if v := find.Search; v != nil && strings.TrimSpace(*v) != "" {
		// Escape LIKE metacharacters so a title containing "_" or "%" matches
		// literally instead of acting as a wildcard.
		pattern := "%" + likePatternEscaper.Replace(strings.TrimSpace(*v)) + "%"
		args = append(args, pattern)
		where = append(where, fmt.Sprintf(
			`(title ILIKE $%d ESCAPE E'\\' OR description ILIKE $%d ESCAPE E'\\')`,
			len(args), len(args)))
	}

	// Soft-deleted rules are invisible unless explicitly requested; archived rules
	// are equally hidden so the default list is the actionable set.
	if !find.IncludeDeleted {
		where = append(where, "deleted = FALSE")
	}
	if !find.IncludeArchived {
		args = append(args, storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED)
		where = append(where, fmt.Sprintf("state <> $%d", len(args)))
	}
	return strings.Join(where, " AND "), args, nil
}

// qualityRuleSemanticChanged reports whether the patch changes the rule
// definition. Only a definition change bumps the revision and appends a
// revision row; display metadata, severity, state, owner, tags and schedule are
// head-only edits.
func qualityRuleSemanticChanged(current *QualityRuleMessage, patch *UpdateQualityRuleMessage) bool {
	if patch.Dimension != nil && *patch.Dimension != current.Dimension {
		return true
	}
	if patch.RuleType != nil && *patch.RuleType != current.RuleType {
		return true
	}
	if patch.TargetGUID != nil && *patch.TargetGUID != current.TargetGUID {
		return true
	}
	if patch.TargetType != nil && *patch.TargetType != current.TargetType {
		return true
	}
	if patch.TargetPath != nil && !proto.Equal(current.TargetPath, patch.TargetPath) {
		return true
	}
	if patch.TargetMetaHash != nil && !bytes.Equal(current.TargetMetaHash, *patch.TargetMetaHash) {
		return true
	}
	if patch.Config != nil && !proto.Equal(current.Config, patch.Config) {
		return true
	}
	return false
}

// CreateQualityRule inserts a rule head and its first revision row in one
// transaction. The caller provides the public resource id: it names the API
// resource and is never reused, so the store must not derive it from the row id.
// The returned message is the stored row, not the input, so database defaults
// (timestamps, state) are reflected.
func (s *Store) CreateQualityRule(ctx context.Context, rule *QualityRuleMessage) (*QualityRuleMessage, error) {
	if rule == nil {
		return nil, common.Errorf(common.Invalid, "a quality rule is required")
	}
	resourceID := strings.TrimSpace(rule.ResourceID)
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "quality rule resource id is required")
	}
	if strings.TrimSpace(rule.TargetGUID) == "" {
		return nil, common.Errorf(common.Invalid, "quality rule target guid is required")
	}
	if rule.Dimension == storepb.QualityDimension_QUALITY_DIMENSION_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality rule dimension is required")
	}
	if rule.RuleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality rule type is required")
	}
	if rule.Severity == storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality rule severity is required")
	}
	if rule.TargetType == storepb.MetaType_UNSPECIFIED {
		return nil, common.Errorf(common.Invalid, "quality rule target type is required")
	}
	if rule.ScheduleInterval < 0 {
		return nil, common.Errorf(common.Invalid, "quality rule schedule interval must not be negative")
	}
	state := rule.State
	if state == storepb.QualityRuleState_QUALITY_RULE_STATE_UNSPECIFIED {
		state = storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
	}
	if !qualityRuleStateIsValid(state) {
		return nil, common.Errorf(common.Invalid, "quality rule state %v is not valid", state)
	}

	targetPath := rule.TargetPath
	if targetPath == nil {
		targetPath = &storepb.QualityTargetPath{}
	}
	config := rule.Config
	if config == nil {
		config = &storepb.QualityRuleConfig{}
	}
	targetPathJSON, err := marshalQualityProto(targetPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality rule target path")
	}
	configJSON, err := marshalQualityProto(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal quality rule config")
	}
	tags := rule.Tags
	if tags == nil {
		tags = []string{}
	}
	intervalSeconds := int32(rule.ScheduleInterval / time.Second)

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin quality rule transaction")
	}
	defer tx.Rollback()

	created, err := scanQualityRule(tx.QueryRowContext(ctx, `
		INSERT INTO quality_rule (
			resource_id, title, description, dimension, rule_type, target_guid,
			target_type, target_path, target_meta_hash, config, severity, state,
			owner_id, tags, schedule_interval_seconds, next_run_at, mute_until,
			revision, created_by, updated_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20
		) RETURNING `+qualityRuleColumns,
		resourceID,
		strings.TrimSpace(rule.Title),
		strings.TrimSpace(rule.Description),
		rule.Dimension,
		rule.RuleType,
		strings.TrimSpace(rule.TargetGUID),
		rule.TargetType,
		targetPathJSON,
		rule.TargetMetaHash,
		configJSON,
		rule.Severity,
		state,
		qualityRuleWriteInt32(rule.OwnerID),
		pq.Array(tags),
		intervalSeconds,
		qualityRuleWriteTime(rule.NextRunAt),
		qualityRuleWriteTime(rule.MuteUntil),
		1,
		qualityRuleWriteInt32(rule.CreatedBy),
		qualityRuleWriteInt32(rule.UpdatedBy),
	))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, common.Errorf(common.Conflict, "quality rule %q already exists", resourceID)
		}
		return nil, errors.Wrap(err, "failed to create quality rule")
	}

	// The head and its revision snapshot must agree from the first write: a
	// result can always be explained by the definition that produced it.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO quality_rule_revision (
			rule_id, revision, target_guid, target_type, target_path,
			target_meta_hash, dimension, rule_type, severity, config, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, created.ID, created.Revision, created.TargetGUID, created.TargetType,
		targetPathJSON, created.TargetMetaHash, created.Dimension, created.RuleType,
		created.Severity, configJSON, qualityRuleWriteInt32(rule.CreatedBy),
	); err != nil {
		return nil, errors.Wrap(err, "failed to create quality rule revision")
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit quality rule transaction")
	}
	return created, nil
}

// GetQualityRule returns the first rule matching the filter, or (nil, nil) when
// none matches. A filter that matches several rows is an error: the caller is
// asking for one rule.
func (s *Store) GetQualityRule(ctx context.Context, find *FindQualityRuleMessage) (*QualityRuleMessage, error) {
	if find == nil {
		return nil, common.Errorf(common.Invalid, "a quality rule filter is required")
	}
	list, err := s.ListQualityRules(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, common.Errorf(common.Internal, "found %d quality rules with the same filter", len(list))
	}
	return list[0], nil
}

// ListQualityRules lists quality rules matching the filter, newest first.
func (s *Store) ListQualityRules(ctx context.Context, find *FindQualityRuleMessage) ([]*QualityRuleMessage, error) {
	if find == nil {
		find = &FindQualityRuleMessage{}
	}
	where, args, err := buildQualityRuleWhere(find)
	if err != nil {
		return nil, err
	}

	query := `SELECT ` + qualityRuleColumns + ` FROM quality_rule WHERE ` + where +
		` ORDER BY created_at DESC, id DESC`
	if find.Limit != nil {
		args = append(args, max(*find.Limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if find.Offset != nil {
		args = append(args, max(*find.Offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality rules")
	}
	defer rows.Close()

	var result []*QualityRuleMessage
	for rows.Next() {
		msg, err := scanQualityRule(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan quality rule")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for quality rules")
	}
	return result, nil
}

// CountQualityRules counts the rules matching the filter. Pagination fields are
// ignored; the count backs the per-table and per-workspace rule limits.
func (s *Store) CountQualityRules(ctx context.Context, find *FindQualityRuleMessage) (int, error) {
	if find == nil {
		find = &FindQualityRuleMessage{}
	}
	where, args, err := buildQualityRuleWhere(find)
	if err != nil {
		return 0, err
	}

	var count int64
	if err := s.GetDB().QueryRowContext(ctx,
		`SELECT count(*) FROM quality_rule WHERE `+where, args...).Scan(&count); err != nil {
		return 0, errors.Wrap(err, "failed to count quality rules")
	}
	return int(count), nil
}

// UpdateQualityRule patches one rule head by its public resource id. A
// definition change (dimension, type, target, target path, target meta hash or
// config) bumps the revision and appends a revision row inside the same
// transaction; every other field is a head-only edit. The row is locked with
// SELECT ... FOR UPDATE, and ExpectedRevision fails the write with
// common.Conflict rather than overwriting a concurrent edit.
func (s *Store) UpdateQualityRule(ctx context.Context, resourceID string, patch *UpdateQualityRuleMessage) (*QualityRuleMessage, error) {
	if patch == nil {
		return nil, common.Errorf(common.Invalid, "a quality rule update patch is required")
	}
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "quality rule resource id is required")
	}
	if patch.State != nil && !qualityRuleStateIsValid(*patch.State) {
		return nil, common.Errorf(common.Invalid, "quality rule state %v is not valid", *patch.State)
	}
	if patch.ScheduleInterval != nil && *patch.ScheduleInterval < 0 {
		return nil, common.Errorf(common.Invalid, "quality rule schedule interval must not be negative")
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin quality rule transaction")
	}
	defer tx.Rollback()

	// Lock the head before reading its revision: the lock is what serializes
	// concurrent editors, and the revision comparison is what tells the caller it
	// lost the race.
	current, err := scanQualityRule(tx.QueryRowContext(ctx, `
		SELECT `+qualityRuleColumns+`
		FROM quality_rule
		WHERE resource_id = $1 AND deleted = FALSE
		FOR UPDATE`, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality rule %q not found", resourceID)
		}
		return nil, errors.Wrap(err, "failed to query quality rule for update")
	}
	if patch.ExpectedRevision != nil && *patch.ExpectedRevision != current.Revision {
		return nil, common.Errorf(common.Conflict,
			"quality rule %q revision changed from %d to %d", resourceID, *patch.ExpectedRevision, current.Revision)
	}

	semanticChanged := qualityRuleSemanticChanged(current, patch)
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	if v := patch.Title; v != nil {
		args = append(args, strings.TrimSpace(*v))
		sets = append(sets, fmt.Sprintf("title = $%d", len(args)))
	}
	if v := patch.Description; v != nil {
		args = append(args, strings.TrimSpace(*v))
		sets = append(sets, fmt.Sprintf("description = $%d", len(args)))
	}
	if v := patch.Dimension; v != nil {
		args = append(args, *v)
		sets = append(sets, fmt.Sprintf("dimension = $%d", len(args)))
	}
	if v := patch.RuleType; v != nil {
		args = append(args, *v)
		sets = append(sets, fmt.Sprintf("rule_type = $%d", len(args)))
	}
	if v := patch.TargetGUID; v != nil {
		args = append(args, strings.TrimSpace(*v))
		sets = append(sets, fmt.Sprintf("target_guid = $%d", len(args)))
	}
	if v := patch.TargetType; v != nil {
		args = append(args, *v)
		sets = append(sets, fmt.Sprintf("target_type = $%d", len(args)))
	}
	if v := patch.TargetPath; v != nil {
		raw, err := marshalQualityProto(v)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal quality rule target path")
		}
		args = append(args, raw)
		sets = append(sets, fmt.Sprintf("target_path = $%d", len(args)))
	}
	if v := patch.TargetMetaHash; v != nil {
		args = append(args, *v)
		sets = append(sets, fmt.Sprintf("target_meta_hash = $%d", len(args)))
	}
	if v := patch.Config; v != nil {
		raw, err := marshalQualityProto(v)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal quality rule config")
		}
		args = append(args, raw)
		sets = append(sets, fmt.Sprintf("config = $%d", len(args)))
	}
	if v := patch.Severity; v != nil {
		args = append(args, *v)
		sets = append(sets, fmt.Sprintf("severity = $%d", len(args)))
	}
	if v := patch.State; v != nil {
		args = append(args, *v)
		sets = append(sets, fmt.Sprintf("state = $%d", len(args)))
	}
	if v := patch.OwnerID; v != nil {
		args = append(args, qualityRuleWriteInt32(v))
		sets = append(sets, fmt.Sprintf("owner_id = $%d", len(args)))
	}
	if v := patch.Tags; v != nil {
		tags := *v
		if tags == nil {
			tags = []string{}
		}
		args = append(args, pq.Array(tags))
		sets = append(sets, fmt.Sprintf("tags = $%d", len(args)))
	}
	if v := patch.ScheduleInterval; v != nil {
		args = append(args, int32(*v/time.Second))
		sets = append(sets, fmt.Sprintf("schedule_interval_seconds = $%d", len(args)))
	}
	if v := patch.NextRunAt; v != nil {
		args = append(args, qualityRuleWriteTime(v))
		sets = append(sets, fmt.Sprintf("next_run_at = $%d", len(args)))
	}
	if v := patch.MuteUntil; v != nil {
		args = append(args, qualityRuleWriteTime(v))
		sets = append(sets, fmt.Sprintf("mute_until = $%d", len(args)))
	}
	if v := patch.UpdatedBy; v != nil {
		args = append(args, qualityRuleWriteInt32(v))
		sets = append(sets, fmt.Sprintf("updated_by = $%d", len(args)))
	}

	if len(args) == 0 {
		// An empty patch (or one carrying only ExpectedRevision) writes nothing.
		return current, nil
	}
	if semanticChanged {
		args = append(args, current.Revision+1)
		sets = append(sets, fmt.Sprintf("revision = $%d", len(args)))
	}

	args = append(args, current.ID)
	updated, err := scanQualityRule(tx.QueryRowContext(ctx, `
		UPDATE quality_rule SET `+strings.Join(sets, ", ")+`
		WHERE id = $`+fmt.Sprintf("%d", len(args))+`
		RETURNING `+qualityRuleColumns, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.NotFound, "quality rule %q not found", resourceID)
		}
		return nil, errors.Wrap(err, "failed to update quality rule")
	}

	if semanticChanged {
		targetPathJSON, err := marshalQualityProto(updated.TargetPath)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal quality rule target path")
		}
		configJSON, err := marshalQualityProto(updated.Config)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal quality rule config")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO quality_rule_revision (
				rule_id, revision, target_guid, target_type, target_path,
				target_meta_hash, dimension, rule_type, severity, config, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, updated.ID, updated.Revision, updated.TargetGUID, updated.TargetType,
			targetPathJSON, updated.TargetMetaHash, updated.Dimension, updated.RuleType,
			updated.Severity, configJSON, qualityRuleWriteInt32(patch.UpdatedBy),
		); err != nil {
			return nil, errors.Wrap(err, "failed to append quality rule revision")
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit quality rule transaction")
	}
	return updated, nil
}

// ArchiveQualityRule soft-deletes a rule: it sets deleted and ARCHIVED and keeps
// the revision, results and issues behind it (the row is never physically
// removed). Archiving an already archived rule returns it unchanged, so the
// operation is idempotent.
func (s *Store) ArchiveQualityRule(ctx context.Context, resourceID string, updatedBy int32) (*QualityRuleMessage, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, common.Errorf(common.Invalid, "quality rule resource id is required")
	}

	archived, err := scanQualityRule(s.GetDB().QueryRowContext(ctx, `
		UPDATE quality_rule
		SET deleted = TRUE,
			state = $2,
			updated_by = $3,
			updated_at = NOW()
		WHERE resource_id = $1 AND deleted = FALSE
		RETURNING `+qualityRuleColumns,
		resourceID,
		storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED,
		qualityRuleWriteInt32(&updatedBy),
	))
	if err == nil {
		return archived, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to archive quality rule")
	}

	// No row was updated: either the rule is already archived (idempotent
	// success) or it does not exist.
	existing, err := s.GetQualityRule(ctx, &FindQualityRuleMessage{
		ResourceID:      &resourceID,
		IncludeDeleted:  true,
		IncludeArchived: true,
	})
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, common.Errorf(common.NotFound, "quality rule %q not found", resourceID)
	}
	return existing, nil
}

// FindDueQualityRules returns the enabled, non-deleted rules whose next_run_at
// has arrived, oldest due first. limit is required and bounds one scheduler tick.
func (s *Store) FindDueQualityRules(ctx context.Context, now time.Time, limit int) ([]*QualityRuleMessage, error) {
	if limit <= 0 {
		return nil, common.Errorf(common.Invalid, "a positive due-rule limit is required")
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityRuleColumns+`
		FROM quality_rule
		WHERE state = $1 AND deleted = FALSE AND next_run_at IS NOT NULL AND next_run_at <= $2
		ORDER BY next_run_at ASC, id ASC
		LIMIT $3`,
		storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED, now, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query due quality rules")
	}
	defer rows.Close()

	var result []*QualityRuleMessage
	for rows.Next() {
		msg, err := scanQualityRule(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan due quality rule")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for due quality rules")
	}
	return result, nil
}

// AdvanceQualityRuleNextRun stamps when the scheduler should consider the rule
// next. Only the schedule columns change: execution never bumps the definition
// revision.
func (s *Store) AdvanceQualityRuleNextRun(ctx context.Context, ruleID int64, nextRunAt time.Time) error {
	if ruleID <= 0 {
		return common.Errorf(common.Invalid, "a quality rule id is required")
	}
	if nextRunAt.IsZero() {
		return common.Errorf(common.Invalid, "a next run time is required")
	}

	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_rule
		SET next_run_at = $2, updated_at = NOW()
		WHERE id = $1`, ruleID, nextRunAt)
	if err != nil {
		return errors.Wrap(err, "failed to advance quality rule next run")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected for the quality rule next run update")
	}
	if affected == 0 {
		return common.Errorf(common.NotFound, "quality rule %d not found", ruleID)
	}
	return nil
}

// ListQualityRuleRevisions lists a rule's definition history, newest revision
// first. Revision rows are append-only; nothing in this file updates or deletes
// one.
func (s *Store) ListQualityRuleRevisions(ctx context.Context, ruleID int64, limit, offset *int) ([]*QualityRuleRevisionMessage, error) {
	if ruleID <= 0 {
		return nil, common.Errorf(common.Invalid, "a quality rule id is required")
	}

	args := []any{ruleID}
	query := `SELECT ` + qualityRuleRevisionColumns + `
		FROM quality_rule_revision
		WHERE rule_id = $1
		ORDER BY revision DESC`
	if limit != nil {
		args = append(args, max(*limit, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset != nil {
		args = append(args, max(*offset, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality rule revisions")
	}
	defer rows.Close()

	var result []*QualityRuleRevisionMessage
	for rows.Next() {
		msg, err := scanQualityRuleRevision(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan quality rule revision")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for quality rule revisions")
	}
	return result, nil
}

// ListQualityRulesByResourceIDs resolves several rules at once for callers that
// hold resource names (result and issue rendering). Deleted rules are excluded,
// because a resolved name must not resurrect a retired rule.
func (s *Store) ListQualityRulesByResourceIDs(ctx context.Context, resourceIDs []string) ([]*QualityRuleMessage, error) {
	if len(resourceIDs) == 0 {
		return []*QualityRuleMessage{}, nil
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityRuleColumns+`
		FROM quality_rule
		WHERE resource_id = ANY($1) AND deleted = FALSE
		ORDER BY created_at DESC, id DESC`, pq.Array(resourceIDs))
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality rules by resource ids")
	}
	defer rows.Close()

	result := make([]*QualityRuleMessage, 0, len(resourceIDs))
	for rows.Next() {
		msg, err := scanQualityRule(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan quality rule")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error for quality rules by resource ids")
	}
	return result, nil
}

// qualityRuleByIDsStatement reads several rules by their numeric primary key. The
// API addresses a rule by its public resource id, but a stored result references
// its rule by the numeric foreign key only, so rendering result rows and the
// rule list's latest-result summary needs this reverse batch lookup.
const qualityRuleByIDsStatement = `
	SELECT ` + qualityRuleColumns + `
	FROM quality_rule
	WHERE id = ANY($1::bigint[])
	ORDER BY id
`

// ListQualityRulesByIDs returns the rules whose ids are listed, ordered by id. An
// empty input is a no-op and never touches the database; the returned slice is
// non-nil so a caller can range over it unconditionally.
//
// Unlike the resource-id lookup, this one does not exclude soft-deleted rules: a
// result or issue keeps pointing at the definition that produced it, and a
// historical row must still render its rule's name.
func (s *Store) ListQualityRulesByIDs(ctx context.Context, ids []int64) ([]*QualityRuleMessage, error) {
	if len(ids) == 0 {
		return []*QualityRuleMessage{}, nil
	}

	rows, err := s.GetDB().QueryContext(ctx, qualityRuleByIDsStatement, pq.Array(ids))
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality rules by ids")
	}
	defer rows.Close()

	result := make([]*QualityRuleMessage, 0, len(ids))
	for rows.Next() {
		msg, err := scanQualityRule(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan quality rule")
		}
		result = append(result, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality rules by ids")
	}
	return result, nil
}

// ListQualityRulesByTargets loads the live rules of several targets in one
// statement. The target directory needs a per-target column count that cannot be
// expressed by the aggregate query without duplicating the config's arm mapping
// in SQL, so the handler reads the page's rules and counts the columns in Go.
func (s *Store) ListQualityRulesByTargets(ctx context.Context, keys []MetaGUIDKey) ([]*QualityRuleMessage, error) {
	if len(keys) == 0 {
		return []*QualityRuleMessage{}, nil
	}
	guids := make([]string, 0, len(keys))
	objectTypes := make([]int16, 0, len(keys))
	for _, key := range keys {
		guids = append(guids, key.GUID)
		objectTypes = append(objectTypes, int16(key.ObjectType))
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT `+qualityRuleColumns+`
		FROM quality_rule r
		JOIN unnest($1::text[], $2::int2[]) AS k(guid, object_type)
			ON r.target_guid = k.guid AND r.target_type = k.object_type
		WHERE r.deleted = FALSE AND r.state <> $3
		ORDER BY r.target_guid, r.id
	`, pq.Array(guids), pq.Array(objectTypes), int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED))
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality rules of the targets")
	}
	defer rows.Close()

	rules := []*QualityRuleMessage{}
	for rows.Next() {
		rule, err := scanQualityRule(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality rule of the targets")
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the quality rules of the targets")
	}
	return rules, nil
}
