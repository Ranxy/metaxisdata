package store

import (
	"context"

	"github.com/lib/pq"
	"github.com/pkg/errors"
)

// The data-quality impact read walks the recorded lineage one level at a time: a
// level is a set of object GUIDs and one statement answers its peers. That is
// cheaper than the graph API's per-node read (one round trip per node) because a
// level costs one index scan per direction, and it is bounded the same way: the
// caller passes the rows it still has budget for.
//
// Both queries drop an edge whose two ends are the same object. A self edge
// carries no impact — the object cannot be affected by itself — and leaving it in
// would only spend the caller's node budget on a peer it has already visited.
const (
	buildListDownstreamLineageGUIDQuery = `
		SELECT DISTINCT target_guid
		FROM column_lineage
		WHERE source_guid = ANY($1)
			AND target_guid <> source_guid
		ORDER BY target_guid
		LIMIT $2
	`

	buildListUpstreamLineageGUIDQuery = `
		SELECT DISTINCT source_guid
		FROM column_lineage
		WHERE target_guid = ANY($1)
			AND source_guid <> target_guid
		ORDER BY source_guid
		LIMIT $2
	`
)

// ListDownstreamLineageGUIDs returns the distinct objects the given objects' data
// reaches directly: the targets of their outgoing lineage edges. Table-level edges
// (the ones that name no column) are included: a table feeding another table is
// recorded exactly by them.
//
// A returned GUID is not checked against the registry; the caller decides which
// peers it can report. An empty input, or a limit of zero, is a no-op.
func (s *Store) ListDownstreamLineageGUIDs(ctx context.Context, guids []string, limit int) ([]string, error) {
	return s.listLineagePeerGUIDs(ctx, buildListDownstreamLineageGUIDQuery, guids, limit)
}

// ListUpstreamLineageGUIDs returns the distinct objects the given objects' data
// comes from directly: the sources of their incoming lineage edges.
func (s *Store) ListUpstreamLineageGUIDs(ctx context.Context, guids []string, limit int) ([]string, error) {
	return s.listLineagePeerGUIDs(ctx, buildListUpstreamLineageGUIDQuery, guids, limit)
}

// listLineagePeerGUIDs runs one level's peer read. The caller passes one of the
// two statements above; only the direction of the index probe differs.
func (s *Store) listLineagePeerGUIDs(ctx context.Context, query string, guids []string, limit int) ([]string, error) {
	if len(guids) == 0 || limit <= 0 {
		return nil, nil
	}
	rows, err := s.GetDB().QueryContext(ctx, query, pq.Array(guids), limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the lineage peers")
	}
	defer rows.Close()

	peers := make([]string, 0, min(limit, len(guids)*4))
	for rows.Next() {
		var guid string
		if err := rows.Scan(&guid); err != nil {
			return nil, errors.Wrap(err, "failed to scan a lineage peer")
		}
		peers = append(peers, guid)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read the lineage peers")
	}
	return peers, nil
}
