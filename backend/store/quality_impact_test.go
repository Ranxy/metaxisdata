package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The failure evidence query is the root-cause read's only window query. Each of
// the predicates below is a way the evidence could silently widen or narrow, and the
// SQL-side cut of the recent ids keeps a noisy target from sending rows the caller
// discards.
func TestQualityFailureEvidenceQueryBoundsTheEvidence(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"(target_guid, target_type::INT) IN (SELECT * FROM unnest($1::text[], $2::INT[]))",
		"AND state = $3",
		"AND status = $4",
		"AND started_at >= $6",
		"AND started_at <= $7",
		"(ARRAY_AGG(id ORDER BY started_at DESC, id DESC))[1:$5]::BIGINT[]",
		"GROUP BY target_guid, target_type",
	} {
		require.Contains(t, qualityFailureEvidenceQuery, want)
	}
}

// Consumption coverage is a different denominator from the overview's coverage:
// only assets another object's lineage reads count, only live instances count, and
// the unmonitored assets must sort before the LIMIT spends rows on monitored ones.
func TestQualityConsumptionCoverageQueryKeepsItsDenominator(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WHERE source_guid <> target_guid",
		"GROUP BY source_guid",
		"JOIN meta_registry_resource r",
		"r.object_type = ANY($1::INT2[])",
		"JOIN instance i",
		"i.deleted = FALSE",
		"q.deleted = FALSE",
		"q.state = $2",
		"COUNT(*) OVER ()",
		"COUNT(*) FILTER (WHERE NOT monitored) OVER ()",
		"ORDER BY monitored ASC, readers DESC, guid ASC",
		"LIMIT $3",
	} {
		require.Contains(t, qualityConsumptionCoverageQuery, want)
	}
}

// A structure change is only reported when the fingerprint moved inside the window,
// the current metadata version began inside it, and an older version exists. Dropping
// the older-version clause would turn a new object into a change; dropping the
// current-version clause would turn an upgrade backfill into one.
func TestMetaRegistrySchemaChangeTimesQueryNeedsAVersionInsideTheWindow(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"JOIN meta_registry_resource_history current",
		"current.valid_to IS NULL",
		"f.updated_at >= $3",
		"f.updated_at <= $4",
		"current.valid_from >= $3",
		"previous.valid_from < $3",
	} {
		require.Contains(t, buildListMetaRegistrySchemaChangeTimesQuery, want)
	}
}

// Both lineage walks read one level per statement, drop a self edge and ask for
// DISTINCT peers; the direction is the only difference between them.
func TestLineagePeerQueriesWalkOneDirection(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query     string
		filter    string
		exclusion string
	}{
		"downstream": {
			query:     buildListDownstreamLineageGUIDQuery,
			filter:    "WHERE source_guid = ANY($1)",
			exclusion: "AND target_guid <> source_guid",
		},
		"upstream": {
			query:     buildListUpstreamLineageGUIDQuery,
			filter:    "WHERE target_guid = ANY($1)",
			exclusion: "AND source_guid <> target_guid",
		},
	}
	for name, test := range tests {
		require.Contains(t, test.query, "SELECT DISTINCT", name)
		require.Contains(t, test.query, test.filter, name)
		require.Contains(t, test.query, test.exclusion, name)
		require.Contains(t, test.query, "ORDER BY", name)
		require.Contains(t, test.query, "LIMIT $2", name)
	}
}

// The empty-input paths return before touching the database, so a caller that found
// nothing to ask about cannot accidentally pay for a query — or, worse, read a whole
// table because an empty set was treated as "no filter".
func TestQualityImpactReadsWithEmptyInputs(t *testing.T) {
	t.Parallel()

	var s *Store
	now := time.Now().UTC()

	evidence, err := s.ListQualityTargetFailureEvidence(context.Background(), nil, now.Add(-time.Hour), now, 3)
	require.NoError(t, err)
	require.Empty(t, evidence)

	changes, err := s.ListMetaRegistrySchemaChangeTimes(context.Background(), nil, now.Add(-time.Hour), now)
	require.NoError(t, err)
	require.Empty(t, changes)

	coverage, err := s.GetQualityConsumptionCoverage(context.Background(), 0)
	require.NoError(t, err)
	require.NotNil(t, coverage)
	require.Zero(t, coverage.ConsumedAssetCount)
	require.Zero(t, coverage.MonitoredAssetCount)
	require.Zero(t, coverage.GapCount)
	require.Empty(t, coverage.Gaps)

	peers, err := s.ListDownstreamLineageGUIDs(context.Background(), nil, 10)
	require.NoError(t, err)
	require.Empty(t, peers)

	peers, err = s.ListUpstreamLineageGUIDs(context.Background(), nil, 0)
	require.NoError(t, err)
	require.Empty(t, peers)
}
