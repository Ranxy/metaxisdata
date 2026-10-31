package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualitySuggestionColumns lists the columns every read of quality_suggestion
// returns, in the order scanQualitySuggestion expects them.
const qualitySuggestionColumns = `id, resource_id, kind, source, target_guid, target_type, payload, signature,
	status, provider, model, prompt_version, created_by, created_at, refreshed_at, decided_by, decided_at,
	decision_note`

// queueQualitySuggestionQuery upserts one pending proposal by identity. A
// suggestion is a proposal for a human, not a log entry: (target_guid,
// target_type, signature) names it, and the partial unique index allows only one
// PENDING row per identity. Re-queueing the same proposal therefore refreshes the
// live row (payload, provider facts, refresh time) instead of appending a
// duplicate, and the database — not a read-then-write race — decides which
// concurrent request wins. `xmax = 0` distinguishes an insert from an update.
const queueQualitySuggestionQuery = `
	INSERT INTO quality_suggestion (
		resource_id, kind, source, target_guid, target_type, payload, signature, status,
		provider, model, prompt_version, created_by, created_at, refreshed_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULL)
	ON CONFLICT (target_guid, target_type, signature) WHERE status = 1
	DO UPDATE SET
		payload = EXCLUDED.payload,
		source = EXCLUDED.source,
		provider = EXCLUDED.provider,
		model = EXCLUDED.model,
		prompt_version = EXCLUDED.prompt_version,
		refreshed_at = EXCLUDED.created_at
	RETURNING ` + qualitySuggestionColumns

// decideQualitySuggestionQuery resolves one suggestion only while it is pending.
// The `status = 1` predicate is the state guard: it is evaluated by the database
// against the current row, so two reviewers deciding the same suggestion cannot
// both succeed, and a decision is never rewritten by a later one.
const decideQualitySuggestionQuery = `
	UPDATE quality_suggestion
	SET status = $2, decided_by = $3, decided_at = $4, decision_note = $5, refreshed_at = NULL
	WHERE resource_id = $1 AND status = 1
	RETURNING ` + qualitySuggestionColumns

// reopenQualitySuggestionQuery puts an accepted suggestion back into the queue.
// The caller uses it when it claimed a draft in order to create a rule and the rule
// write then failed: the claim is not a decision the reviewer made, so leaving the
// draft accepted would hide it without a rule behind it. The ACCEPTED predicate is
// the guard; reopening is only ever a rollback of this caller's own claim.
const reopenQualitySuggestionQuery = `
	UPDATE quality_suggestion
	SET status = 1, decided_by = NULL, decided_at = NULL, decision_note = '', refreshed_at = NULL
	WHERE resource_id = $1 AND status = 2
	RETURNING ` + qualitySuggestionColumns

// QualitySuggestionMessage is one row of quality_suggestion: a model-proposed
// rule draft or semantic label waiting for a reviewer. Nothing in this table is a
// rule; accepting a suggestion edits or creates one through the normal rule path.
type QualitySuggestionMessage struct {
	ID            int64
	ResourceID    string
	Kind          storepb.QualitySuggestionKind
	Source        storepb.QualitySuggestionSource
	TargetGUID    string
	TargetType    storepb.MetaType
	Payload       *storepb.QualitySuggestionPayload
	Signature     string
	Status        storepb.QualitySuggestionStatus
	Provider      string
	Model         string
	PromptVersion string
	CreatedBy     *int32
	CreatedAt     time.Time
	// RefreshedAt is set when a later generation pass updated this proposal.
	RefreshedAt  *time.Time
	DecidedBy    *int32
	DecidedAt    *time.Time
	DecisionNote string
}

// FindQualitySuggestionMessage filters suggestions. Every field is optional for a
// list; a get must name the resource.
type FindQualitySuggestionMessage struct {
	ResourceID *string
	Statuses   []storepb.QualitySuggestionStatus
	Kinds      []storepb.QualitySuggestionKind
	Sources    []storepb.QualitySuggestionSource
	TargetGUID *string
	// Instance, Database, Schema and TablePrefix are GUID path prefixes, the same
	// shape as the rule list filters: a suggestion matches when its target GUID
	// starts with the prefix.
	Instance    string
	Database    string
	Schema      string
	TablePrefix string
	Limit       *int
	Offset      *int
}

// qualitySuggestionWhere builds the predicate and positional arguments for a
// find. Every value is bound and the compared columns are the table's own; the
// path filters reuse the rules' GUID subtree semantics.
func qualitySuggestionWhere(find *FindQualitySuggestionMessage) (string, []any, error) {
	where := []string{"TRUE"}
	args := []any{}
	if find == nil {
		return strings.Join(where, " AND "), args, nil
	}
	if find.ResourceID != nil {
		args = append(args, *find.ResourceID)
		where = append(where, fmt.Sprintf("resource_id = $%d", len(args)))
	}
	if len(find.Statuses) > 0 {
		statuses := make([]int32, 0, len(find.Statuses))
		for _, status := range find.Statuses {
			statuses = append(statuses, int32(status))
		}
		args = append(args, pq.Array(statuses))
		where = append(where, fmt.Sprintf("status = ANY($%d::int[])", len(args)))
	}
	if len(find.Kinds) > 0 {
		kinds := make([]int32, 0, len(find.Kinds))
		for _, kind := range find.Kinds {
			kinds = append(kinds, int32(kind))
		}
		args = append(args, pq.Array(kinds))
		where = append(where, fmt.Sprintf("kind = ANY($%d::int[])", len(args)))
	}
	if len(find.Sources) > 0 {
		sources := make([]int32, 0, len(find.Sources))
		for _, source := range find.Sources {
			sources = append(sources, int32(source))
		}
		args = append(args, pq.Array(sources))
		where = append(where, fmt.Sprintf("source = ANY($%d::int[])", len(args)))
	}
	if find.TargetGUID != nil && strings.TrimSpace(*find.TargetGUID) != "" {
		where, args = appendGUIDSubtreeCondition(where, args, "target_guid", strings.TrimSpace(*find.TargetGUID))
	}
	prefix, err := metaGUIDPrefix(find.Instance, find.Database, find.Schema, find.TablePrefix)
	if err != nil {
		return "", nil, err
	}
	if prefix != "" {
		where, args = appendGUIDSubtreeCondition(where, args, "target_guid", prefix)
	}
	return strings.Join(where, " AND "), args, nil
}

// qualitySuggestionQuery assembles one filtered, ordered suggestion read.
func qualitySuggestionQuery(find *FindQualitySuggestionMessage) (string, []any, error) {
	where, args, err := qualitySuggestionWhere(find)
	if err != nil {
		return "", nil, err
	}
	query := `SELECT ` + qualitySuggestionColumns + ` FROM quality_suggestion WHERE ` + where +
		` ORDER BY created_at DESC, id DESC`
	if find != nil && find.Limit != nil {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, *find.Limit)
	}
	if find != nil && find.Offset != nil {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, *find.Offset)
	}
	return query, args, nil
}

// scanQualitySuggestion maps one quality_suggestion row. The nullable review
// columns stay nil pointers, so "not yet reviewed" is distinct from a decision
// stamped at the zero time.
func scanQualitySuggestion(row rowScanner) (*QualitySuggestionMessage, error) {
	var (
		message     QualitySuggestionMessage
		kind        int16
		source      int16
		targetType  int16
		payload     []byte
		status      int16
		createdBy   sql.NullInt32
		refreshedAt sql.NullTime
		decidedBy   sql.NullInt32
		decidedAt   sql.NullTime
	)
	if err := row.Scan(
		&message.ID, &message.ResourceID, &kind, &source, &message.TargetGUID, &targetType, &payload,
		&message.Signature, &status, &message.Provider, &message.Model, &message.PromptVersion, &createdBy,
		&message.CreatedAt, &refreshedAt, &decidedBy, &decidedAt, &message.DecisionNote,
	); err != nil {
		return nil, err
	}
	message.Kind = storepb.QualitySuggestionKind(kind)
	message.Source = storepb.QualitySuggestionSource(source)
	message.TargetType = storepb.MetaType(targetType)
	message.Status = storepb.QualitySuggestionStatus(status)
	messagePayload := &storepb.QualitySuggestionPayload{}
	if len(payload) > 0 {
		if err := common.ProtojsonUnmarshaler.Unmarshal(payload, messagePayload); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal a quality suggestion payload")
		}
	}
	message.Payload = messagePayload
	if createdBy.Valid {
		value := createdBy.Int32
		message.CreatedBy = &value
	}
	if refreshedAt.Valid {
		value := refreshedAt.Time
		message.RefreshedAt = &value
	}
	if decidedBy.Valid {
		value := decidedBy.Int32
		message.DecidedBy = &value
	}
	if decidedAt.Valid {
		value := decidedAt.Time
		message.DecidedAt = &value
	}
	return &message, nil
}

// validateQualitySuggestion checks what a suggestion row cannot be stored
// without, including that the payload arm matches the suggestion kind: a
// RULE_DRAFT carries a rule draft and a LABEL carries a label, so a reviewer is
// never shown an empty card.
func validateQualitySuggestion(message *QualitySuggestionMessage) error {
	if message == nil {
		return common.Errorf(common.Invalid, "quality suggestion is required")
	}
	if strings.TrimSpace(message.ResourceID) == "" {
		return common.Errorf(common.Invalid, "quality suggestion resource id is required")
	}
	if message.Kind == storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_UNSPECIFIED {
		return common.Errorf(common.Invalid, "quality suggestion kind is required")
	}
	if message.Source == storepb.QualitySuggestionSource_QUALITY_SUGGESTION_SOURCE_UNSPECIFIED {
		return common.Errorf(common.Invalid, "quality suggestion source is required")
	}
	if strings.TrimSpace(message.TargetGUID) == "" {
		return common.Errorf(common.Invalid, "quality suggestion target guid is required")
	}
	if message.TargetType == storepb.MetaType_UNSPECIFIED {
		return common.Errorf(common.Invalid, "quality suggestion target type is required")
	}
	// The signature is the proposal identity. A row without one could not be
	// deduplicated, so it is refused rather than stored as an anonymous duplicate.
	if strings.TrimSpace(message.Signature) == "" {
		return common.Errorf(common.Invalid, "quality suggestion signature is required")
	}
	if message.Payload == nil {
		return common.Errorf(common.Invalid, "quality suggestion payload is required")
	}
	switch message.Kind {
	case storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT:
		if message.Payload.GetRule() == nil {
			return common.Errorf(common.Invalid, "a rule draft suggestion must carry a rule payload")
		}
	case storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL:
		if message.Payload.GetLabel() == nil {
			return common.Errorf(common.Invalid, "a label suggestion must carry a label payload")
		}
	case storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_UNSPECIFIED:
		// Rejected above; listed so the switch covers every enum value.
		return common.Errorf(common.Invalid, "quality suggestion kind is required")
	default:
		return common.Errorf(common.Invalid, "unknown quality suggestion kind %d", message.Kind)
	}
	return nil
}

// validateSuggestionDecision checks the only transition the review queue allows:
// a pending suggestion becomes accepted or rejected, by a known principal. The
// store refuses to create a suggestion that is already decided, so the guard here
// and the `status = 1` predicate in the update are the same invariant at both
// ends.
func validateSuggestionDecision(resourceID string, status storepb.QualitySuggestionStatus, decidedBy int32) error {
	if strings.TrimSpace(resourceID) == "" {
		return common.Errorf(common.Invalid, "quality suggestion resource id is required")
	}
	if status != storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED &&
		status != storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED {
		return common.Errorf(common.Invalid, "a suggestion can only be accepted or rejected")
	}
	if decidedBy <= 0 {
		return common.Errorf(common.Invalid, "quality suggestion decider is required")
	}
	return nil
}

// validateFindQualitySuggestion keeps a get pointed at exactly one suggestion: by
// target or status multiple rows are expected, so only a resource-scoped get is
// allowed and the queue is read with ListQualitySuggestions.
func validateFindQualitySuggestion(find *FindQualitySuggestionMessage) error {
	if find == nil || find.ResourceID == nil || strings.TrimSpace(*find.ResourceID) == "" {
		return common.Errorf(common.Invalid, "quality suggestion resource id is required")
	}
	return nil
}

// qualitySuggestionInitialStatus resolves the status a new suggestion is stored
// with: it is always PENDING, because a suggestion exists to be reviewed and a
// decision is recorded through DecideQualitySuggestion, which carries the
// reviewer, the instant and the note.
func qualitySuggestionInitialStatus(status storepb.QualitySuggestionStatus) (storepb.QualitySuggestionStatus, error) {
	if status == storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED ||
		status == storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING {
		return storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING, nil
	}
	return storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED,
		common.Errorf(common.Invalid, "a quality suggestion is created pending")
}

// QueueQualitySuggestion inserts or refreshes the one live proposal for this
// identity and reports whether it was created. A caller that leaves the status
// unset gets PENDING, and a caller that asks for an already-decided status is
// refused: a decision is recorded through DecideQualitySuggestion, which carries
// the reviewer and the note.
//
// Refreshing keeps the row id and create time, so a reviewer's open page keeps the
// same resource name, and it moves the payload to the newest proposal. The
// database's partial unique index is what makes concurrent generation passes
// converge on one row.
func (s *Store) QueueQualitySuggestion(ctx context.Context, message *QualitySuggestionMessage) (*QualitySuggestionMessage, bool, error) {
	if err := validateQualitySuggestion(message); err != nil {
		return nil, false, err
	}
	status, err := qualitySuggestionInitialStatus(message.Status)
	if err != nil {
		return nil, false, err
	}

	payloadJSON, err := protojson.Marshal(message.Payload)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to marshal a quality suggestion payload")
	}

	createdAt := message.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}

	stored, err := scanQualitySuggestion(s.GetDB().QueryRowContext(ctx, queueQualitySuggestionQuery,
		message.ResourceID, int32(message.Kind), int32(message.Source), message.TargetGUID,
		int32(message.TargetType), payloadJSON, message.Signature, int32(status), message.Provider,
		message.Model, message.PromptVersion, message.CreatedBy, createdAt,
	))
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to queue a quality suggestion")
	}
	// An inserted row has no refresh time; an updated one was stamped with the
	// current pass's instant by the upsert.
	return stored, stored.RefreshedAt == nil, nil
}

// GetQualitySuggestion returns the suggestion the find names, or (nil, nil) when
// it does not exist.
func (s *Store) GetQualitySuggestion(ctx context.Context, find *FindQualitySuggestionMessage) (*QualitySuggestionMessage, error) {
	if err := validateFindQualitySuggestion(find); err != nil {
		return nil, err
	}
	list, err := s.ListQualitySuggestions(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > 1 {
		return nil, common.Errorf(common.Internal, "found %d quality suggestions for one resource id, expect 1", len(list))
	}
	return list[0], nil
}

// ListQualitySuggestions lists suggestions matching the filter, newest first.
func (s *Store) ListQualitySuggestions(ctx context.Context, find *FindQualitySuggestionMessage) ([]*QualitySuggestionMessage, error) {
	db := s.GetDB()

	query, args, err := qualitySuggestionQuery(find)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query quality suggestions")
	}
	defer rows.Close()

	var suggestions []*QualitySuggestionMessage
	for rows.Next() {
		suggestion, err := scanQualitySuggestion(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality suggestion")
		}
		suggestions = append(suggestions, suggestion)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality suggestions")
	}
	return suggestions, nil
}

// DecideQualitySuggestion accepts or rejects one pending suggestion. The state
// transition is performed by a single conditional UPDATE, so a suggestion can be
// decided exactly once even under concurrent reviewers; a suggestion that is
// already decided is a conflict, and a resource that does not exist is a
// not-found.
func (s *Store) DecideQualitySuggestion(ctx context.Context, resourceID string, status storepb.QualitySuggestionStatus, decidedBy int32, note string, at time.Time) (*QualitySuggestionMessage, error) {
	if err := validateSuggestionDecision(resourceID, status, decidedBy); err != nil {
		return nil, err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}

	suggestion, err := scanQualitySuggestion(s.GetDB().QueryRowContext(ctx, decideQualitySuggestionQuery,
		resourceID, int32(status), decidedBy, at, strings.TrimSpace(note)))
	if err == nil {
		return suggestion, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to decide a quality suggestion")
	}

	// Nothing was updated: either the suggestion does not exist, or it is not
	// pending any more. The distinction is the caller's: absent is not found,
	// already decided is a conflict.
	existing, err := s.GetQualitySuggestion(ctx, &FindQualitySuggestionMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, common.Errorf(common.NotFound, "quality suggestion %q not found", resourceID)
	}
	return nil, common.Errorf(common.Conflict, "quality suggestion %q is already decided", resourceID)
}

// ReopenQualitySuggestion returns an accepted suggestion to PENDING. It is the
// rollback half of claiming a draft to create a rule from it: if the rule write
// fails, the claim is undone so the reviewer can retry instead of finding the
// draft silently accepted with no rule behind it.
func (s *Store) ReopenQualitySuggestion(ctx context.Context, resourceID string) (*QualitySuggestionMessage, error) {
	if strings.TrimSpace(resourceID) == "" {
		return nil, common.Errorf(common.Invalid, "quality suggestion resource id is required")
	}
	suggestion, err := scanQualitySuggestion(s.GetDB().QueryRowContext(ctx, reopenQualitySuggestionQuery, resourceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, common.Errorf(common.Conflict, "quality suggestion %q is not accepted and cannot be reopened", resourceID)
		}
		return nil, errors.Wrap(err, "failed to reopen a quality suggestion")
	}
	return suggestion, nil
}

// SupersedePendingQualitySuggestions closes the still-pending proposals with the
// given identities because their fact now exists or was replaced. It closes them
// without a reviewer decision, which is why the status is SUPERSEDED rather than
// ACCEPTED/REJECTED, and it records the reason in the note. A proposal that is not
// pending is left untouched, so this is safe to call on every write path.
func (s *Store) SupersedePendingQualitySuggestions(ctx context.Context, targetGUID string, targetType storepb.MetaType, signatures []string, reason string, at time.Time) (int64, error) {
	if len(signatures) == 0 {
		return 0, nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE quality_suggestion
		SET status = $1, decided_at = $2, decision_note = $3
		WHERE target_guid = $4 AND target_type = $5 AND status = $6 AND signature = ANY($7::text[])
	`,
		int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_SUPERSEDED), at, strings.TrimSpace(reason),
		targetGUID, int16(targetType),
		int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING),
		pq.Array(signatures))
	if err != nil {
		return 0, errors.Wrap(err, "failed to supersede pending quality suggestions")
	}
	return result.RowsAffected()
}

// QualitySuggestionSignatureState is what the queue knows about one proposal
// identity on a target: whether a live proposal is waiting, and what a reviewer
// last decided about it. Generation uses it to avoid re-proposing a decided
// proposal and to skip spending a model call on one that is already pending.
type QualitySuggestionSignatureState struct {
	Pending      bool
	LastDecision storepb.QualitySuggestionStatus
}

// QualitySuggestionSignatureStates folds the queue into one state per signature.
// The read is scoped to a target and to the signatures the caller is considering,
// so it stays small even when the target has a long decision history.
func (s *Store) QualitySuggestionSignatureStates(ctx context.Context, targetGUID string, targetType storepb.MetaType, signatures []string) (map[string]QualitySuggestionSignatureState, error) {
	states := make(map[string]QualitySuggestionSignatureState, len(signatures))
	if len(signatures) == 0 {
		return states, nil
	}
	pending := int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING)
	accepted := int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED)
	rejected := int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED)
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT signature,
			COALESCE(bool_or(status = $1), FALSE),
			(array_agg(status ORDER BY decided_at DESC NULLS LAST, id DESC)
				FILTER (WHERE status IN ($2, $3)))[1]
		FROM quality_suggestion
		WHERE target_guid = $4 AND target_type = $5 AND signature = ANY($6::text[])
		GROUP BY signature
	`, pending, accepted, rejected, targetGUID, int16(targetType), pq.Array(signatures))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read quality suggestion signature states")
	}
	defer rows.Close()
	for rows.Next() {
		var (
			signature string
			isPending bool
			last      sql.NullInt16
		)
		if err := rows.Scan(&signature, &isPending, &last); err != nil {
			return nil, errors.Wrap(err, "failed to scan a quality suggestion signature state")
		}
		state := QualitySuggestionSignatureState{Pending: isPending}
		if last.Valid {
			state.LastDecision = storepb.QualitySuggestionStatus(last.Int16)
		}
		states[signature] = state
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate quality suggestion signature states")
	}
	return states, nil
}

// DeleteExpiredQualitySuggestions removes the decided suggestions older than
// cutoff. A pending suggestion is never pruned: it is still waiting for a
// reviewer.
func (s *Store) DeleteExpiredQualitySuggestions(ctx context.Context, cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, common.Errorf(common.Invalid, "a cutoff is required")
	}
	result, err := s.GetDB().ExecContext(ctx, `
		DELETE FROM quality_suggestion
		WHERE status <> $1 AND decided_at IS NOT NULL AND decided_at < $2
	`, int16(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING), cutoff)
	if err != nil {
		return 0, errors.Wrap(err, "failed to delete expired quality suggestions")
	}
	return result.RowsAffected()
}
