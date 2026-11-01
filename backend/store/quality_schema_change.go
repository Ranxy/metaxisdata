package store

import (
	"context"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// QualitySchemaChangeTarget is one monitored target a structure-change event is
// addressed to. It carries what the notifier needs and nothing else: who owns
// the target's live checks, and whether it has any.
type QualitySchemaChangeTarget struct {
	// OwnerIDs are the distinct owners of the target's non-archived checks, in no
	// particular order. Empty when every check is unassigned.
	OwnerIDs []int32
	// RuleCount is how many non-archived checks the target has.
	RuleCount int32
}

// buildListQualitySchemaChangeTargetsQuery lists the monitored targets among the
// requested keys. The paired predicate keeps the request from matching every
// cross combination of the two arrays, like the other key-based reads.
func buildListQualitySchemaChangeTargetsQuery() string {
	return `
		SELECT target_guid, target_type,
			(COUNT(*))::INT AS rule_count,
			COALESCE(
				(ARRAY_AGG(DISTINCT owner_id) FILTER (WHERE owner_id IS NOT NULL))::INT[],
				'{}'
			) AS owner_ids
		FROM quality_rule
		WHERE deleted = FALSE
			AND state <> $3
			AND (target_guid, target_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))
		GROUP BY target_guid, target_type
	`
}

// buildBaselineQualityRulesToSchemaHashQuery points the target's checks at a new
// structure snapshot. It touches only target_meta_hash and updated_at: no
// revision row is written, because the check's definition did not change.
func buildBaselineQualityRulesToSchemaHashQuery() string {
	return `
		UPDATE quality_rule
		SET target_meta_hash = $3, updated_at = now()
		WHERE deleted = FALSE
			AND state <> $4
			AND target_guid = $1
			AND target_type = $2
			AND target_meta_hash IS DISTINCT FROM $3
	`
}

// ListQualitySchemaChangeTargets returns the monitored targets among keys. A key
// with no non-archived quality_rule is absent from the result: nobody watches it,
// so a structure change there is not an event anyone is told about.
func (s *Store) ListQualitySchemaChangeTargets(ctx context.Context, keys []MetaGUIDKey) (map[MetaGUIDKey]QualitySchemaChangeTarget, error) {
	result := make(map[MetaGUIDKey]QualitySchemaChangeTarget, len(keys))
	if len(keys) == 0 {
		return result, nil
	}

	guids, objectTypes := metaGUIDKeyArrays(keys)
	rows, err := s.GetDB().QueryContext(ctx, buildListQualitySchemaChangeTargetsQuery(),
		pq.Array(guids), pq.Array(objectTypes), int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED))
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the targets' quality rules")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			guid       string
			objectType int32
			ruleCount  int32
			ownerIDs   pq.Int32Array
		)
		if err := rows.Scan(&guid, &objectType, &ruleCount, &ownerIDs); err != nil {
			return nil, errors.Wrap(err, "failed to scan a target's quality rules")
		}
		result[MetaGUIDKey{GUID: guid, ObjectType: storepb.MetaType(objectType)}] = QualitySchemaChangeTarget{
			OwnerIDs:  ownerIDs,
			RuleCount: ruleCount,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read the targets' quality rules")
	}
	return result, nil
}

// BaselineQualityRulesToSchemaHash points every non-archived check of a target
// at the given schema fingerprint, clearing the schema-changed signal for a
// target whose new shape a reviewer accepted.
//
// It deliberately does not bump a revision: a rule's revision is the history of
// its definition, and re-baselining changes which shape the definition was
// reviewed against, not the definition. It returns how many rules moved.
func (s *Store) BaselineQualityRulesToSchemaHash(ctx context.Context, targetGUID string, targetType storepb.MetaType, schemaHash []byte) (int64, error) {
	if len(schemaHash) == 0 {
		return 0, errors.New("a schema fingerprint is required to re-baseline a target")
	}
	result, err := s.GetDB().ExecContext(ctx, buildBaselineQualityRulesToSchemaHashQuery(),
		targetGUID, int16(targetType), schemaHash, int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED))
	if err != nil {
		return 0, errors.Wrap(err, "failed to re-baseline the target's quality rules")
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to count the re-baselined quality rules")
	}
	return updated, nil
}
