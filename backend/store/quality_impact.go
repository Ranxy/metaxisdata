package store

import (
	"context"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// qualityFailureEvidenceQuery aggregates one target's failing executions inside a
// window. It is the root-cause read's only window query: one statement answers
// every upstream target, the target predicate probes
// idx_quality_execution_target_time, and the aggregate keeps the result one row per
// target rather than one row per run.
//
// The recent executions are cut in SQL so a target that failed every ten minutes
// for a day does not send a thousand ids over the wire to be discarded by a
// three-item cap in Go.
const qualityFailureEvidenceQuery = `
	SELECT target_guid,
		target_type,
		COUNT(DISTINCT rule_id)::INT AS failing_rule_count,
		COUNT(*)::INT AS failing_execution_count,
		MAX(started_at) AS latest_failure_time,
		(ARRAY_AGG(id ORDER BY started_at DESC, id DESC))[1:$5]::BIGINT[] AS recent_execution_ids
	FROM quality_execution
	WHERE (target_guid, target_type::INT) IN (SELECT * FROM unnest($1::text[], $2::INT[]))
		AND state = $3
		AND status = $4
		AND started_at >= $6
		AND started_at <= $7
	GROUP BY target_guid, target_type
`

// QualityFailureEvidence is what one upstream target contributed to the
// root-cause window.
type QualityFailureEvidence struct {
	// FailingRuleCount counts distinct checks that failed; FailingExecutionCount
	// counts runs, so a check that failed three times counts once and three.
	FailingRuleCount      int32
	FailingExecutionCount int32
	LatestFailureTime     time.Time
	// RecentExecutionIDs are the newest failing runs, newest first, at most the
	// limit the caller passed. They are row ids; the API turns them into resource
	// names.
	RecentExecutionIDs []int64
}

// ListQualityTargetFailureEvidence reports, for each requested target, its
// completed FAIL executions inside [from, to]. A target with no failing run in the
// window is absent from the result rather than present with zeros.
func (s *Store) ListQualityTargetFailureEvidence(ctx context.Context, keys []MetaGUIDKey, from, to time.Time, recentLimit int) (map[MetaGUIDKey]*QualityFailureEvidence, error) {
	result := make(map[MetaGUIDKey]*QualityFailureEvidence, len(keys))
	if len(keys) == 0 || recentLimit <= 0 {
		return result, nil
	}

	guids, objectTypes := metaGUIDKeyArrays(keys)
	rows, err := s.GetDB().QueryContext(ctx, qualityFailureEvidenceQuery,
		pq.Array(guids), pq.Array(objectTypes),
		int16(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED),
		int16(storepb.QualityStatus_QUALITY_STATUS_FAIL),
		recentLimit, from, to,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the targets' failing executions")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			guid       string
			objectType int32
			evidence   QualityFailureEvidence
			recentIDs  pq.Int64Array
		)
		if err := rows.Scan(&guid, &objectType, &evidence.FailingRuleCount,
			&evidence.FailingExecutionCount, &evidence.LatestFailureTime, &recentIDs); err != nil {
			return nil, errors.Wrap(err, "failed to scan a target's failing executions")
		}
		evidence.RecentExecutionIDs = recentIDs
		result[MetaGUIDKey{GUID: guid, ObjectType: storepb.MetaType(objectType)}] = &evidence
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read the targets' failing executions")
	}
	return result, nil
}

// qualityConsumptionCoverageQuery answers the consumption-coverage read in one
// statement: the lineage table is scanned once, grouped by the asset that its
// edges read from, and joined to the registry (the object must be a monitorable
// type under a live instance) and to the rules (monitored means at least one
// enabled check).
//
// The window counts are computed before the LIMIT so the totals, which a reader
// needs to judge the returned gaps, come back with the same scan that ranks them.
// Ordering puts the gaps first — unmonitored before monitored, then by how many
// objects read the asset — so the caller's limit spends the statement's rows on
// the rows worth showing.
const qualityConsumptionCoverageQuery = `
	WITH consumed AS (
		SELECT source_guid AS guid, COUNT(DISTINCT target_guid)::INT AS readers
		FROM column_lineage
		WHERE source_guid <> target_guid
		GROUP BY source_guid
	), assets AS (
		SELECT c.guid,
			r.object_type::INT AS object_type,
			c.readers,
			EXISTS (
				SELECT 1
				FROM quality_rule q
				WHERE q.target_guid = c.guid
					AND q.target_type = r.object_type
					AND q.deleted = FALSE
					AND q.state = $2
			) AS monitored
		FROM consumed c
		JOIN meta_registry_resource r
			ON r.guid = c.guid AND r.object_type = ANY($1::INT2[])
		JOIN instance i
			ON i.resource_id = split_part(r.guid, ';', 1) AND i.deleted = FALSE
	)
	SELECT guid,
		object_type,
		readers,
		monitored,
		(COUNT(*) OVER ())::INT AS consumed_asset_count,
		(COUNT(*) FILTER (WHERE NOT monitored) OVER ())::INT AS gap_count
	FROM assets
	ORDER BY monitored ASC, readers DESC, guid ASC
	LIMIT $3
`

// QualityConsumptionGap is one consumed asset no enabled check watches.
type QualityConsumptionGap struct {
	GUID string
	// ObjectType is the registry type the asset was counted as.
	ObjectType storepb.MetaType
	// DownstreamAssetCount is how many distinct objects its lineage reads it.
	DownstreamAssetCount int32
}

// QualityConsumptionCoverage is the workspace's consumption-coverage answer: the
// assets other objects read, how many of them an enabled check watches, and the
// unwatched ones with the most consumers.
type QualityConsumptionCoverage struct {
	ConsumedAssetCount  int32
	MonitoredAssetCount int32
	GapCount            int32
	Gaps                []*QualityConsumptionGap
}

// GetQualityConsumptionCoverage measures how much of the data other assets consume
// is watched. The denominator is deliberately not the overview coverage's: there
// every monitorable registry object counts, here only objects that at least one
// other object's lineage reads.
//
// gapLimit bounds the returned gaps, not the scan: the counts always cover the
// whole workspace.
func (s *Store) GetQualityConsumptionCoverage(ctx context.Context, gapLimit int) (*QualityConsumptionCoverage, error) {
	result := &QualityConsumptionCoverage{}
	if gapLimit <= 0 {
		return result, nil
	}
	rows, err := s.GetDB().QueryContext(ctx, qualityConsumptionCoverageQuery,
		pq.Array(qualityCoverageObjectTypes),
		int16(storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED),
		gapLimit,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the consumption coverage")
	}
	defer rows.Close()

	for rows.Next() {
		var (
			guid       string
			objectType int32
			readers    int32
			monitored  bool
			consumed   int32
			gaps       int32
		)
		if err := rows.Scan(&guid, &objectType, &readers, &monitored,
			&consumed, &gaps); err != nil {
			return nil, errors.Wrap(err, "failed to scan a consumption coverage row")
		}
		result.ConsumedAssetCount = consumed
		result.GapCount = gaps
		if monitored {
			continue
		}
		result.Gaps = append(result.Gaps, &QualityConsumptionGap{
			GUID:                 guid,
			ObjectType:           storepb.MetaType(objectType),
			DownstreamAssetCount: readers,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read the consumption coverage")
	}
	result.MonitoredAssetCount = result.ConsumedAssetCount - result.GapCount
	return result, nil
}
