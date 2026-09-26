package openlineage

import (
	"context"
	"log/slog"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// revalidationBatch bounds how many pending edges one pass reads, so a backlog
// cannot turn a periodic pass into a table scan.
const revalidationBatch = 500

// RevalidateUnresolvedLineage re-checks the edges whose endpoints the registry did
// not know when they were stored.
//
// An ingested edge is kept when its relation is missing, because ingestion runs
// before the next schema sync and dropping it would lose the lineage of a table
// created minutes ago. That keeps real lineage, but it also means the edge's
// columns were never checked and the edge may name a relation that never appears.
// Once the relation does appear - or once a second look proves it is still absent -
// the same validation the ingestion path runs decides what the edge should say
// now: a column the relation does not have is blanked, and the object types are
// filled in. Edges whose relation is still unknown stay, and are counted so the
// backlog is visible instead of silent.
//
// It returns the number of edges re-checked and the number of meta objects that
// still reference an unknown relation.
func (p *Processor) RevalidateUnresolvedLineage(ctx context.Context, limit int) (revalidated, stillUnknown int, err error) {
	if limit <= 0 {
		limit = revalidationBatch
	}
	pending, err := p.store.FindColumnLineageWithUnknownEndpoint(ctx, ExternalDatasetGUIDPrefix, limit)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to list lineage with an unknown endpoint")
	}

	// A run's edges are replaced as a set, so every edge of an affected object is
	// re-validated together; replacing only the pending subset would drop the rest.
	type metaKey struct {
		guid     string
		metaType storepb.MetaType
	}
	affected := make(map[metaKey]struct{}, len(pending))
	for _, edge := range pending {
		if edge.MetaType != storepb.MetaType_OPENLINEAGE {
			continue
		}
		affected[metaKey{guid: edge.MetaGUID, metaType: edge.MetaType}] = struct{}{}
	}

	var degraded int
	for key := range affected {
		metaGUID, metaType := key.guid, key.metaType
		edges, err := p.store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{
			MetaGUID: &metaGUID,
			MetaType: &metaType,
		})
		if err != nil {
			return revalidated, stillUnknown, errors.Wrapf(err, "failed to list lineage of %s", metaGUID)
		}
		kept, report, err := p.validateIngestedLineage(ctx, edges)
		if err != nil {
			return revalidated, stillUnknown, errors.Wrapf(err, "failed to validate lineage of %s", metaGUID)
		}
		if err := p.store.BatchReplaceColumnLineage(ctx, metaGUID, metaType, kept); err != nil {
			return revalidated, stillUnknown, errors.Wrapf(err, "failed to store revalidated lineage of %s", metaGUID)
		}
		revalidated += len(kept)
		degraded += report.degradedEdges
		if report.unknownRelationEdges > 0 {
			stillUnknown++
		}
	}

	if len(affected) > 0 {
		slog.Info("Revalidated ingested lineage whose endpoints the registry did not know",
			"pendingEdges", len(pending),
			"objects", len(affected),
			"revalidatedEdges", revalidated,
			"degradedEdges", degraded,
			"objectsStillUnknown", stillUnknown,
		)
	}
	return revalidated, stillUnknown, nil
}
