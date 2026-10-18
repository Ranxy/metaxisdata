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

// metaKey identifies the object a run's edges are stored under.
type metaKey struct {
	guid     string
	metaType storepb.MetaType
}

// acquireRevalidation serializes revalidation passes without an uncancellable
// wait: a pass that arrives while another is running either takes the semaphore
// once it is released or returns ctx.Err() when its context ends first. A plain
// mutex would let shutdown block on a queued pass and would keep a pass timeout
// from bounding the wait.
func (p *Processor) acquireRevalidation(ctx context.Context) error {
	select {
	case p.revalidateSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseRevalidation frees the semaphore taken by acquireRevalidation.
func (p *Processor) releaseRevalidation() {
	<-p.revalidateSem
}

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
// It returns the number of edges re-checked, the number of meta objects that
// still reference an unknown relation, and whether the pass filled its batch limit
// with edges it can revalidate. A true hasMore means another batch may be waiting,
// so a caller draining a backlog calls again until it is false or its own pass
// deadline ends. A full batch of another producer's edges reports false: this pass
// leaves those alone, so calling again would read the same rows and make no
// progress.
//
// limit bounds how many pending edges one pass reads; 0 means revalidationBatch,
// the implementation's default. One pass never reads more than one batch: the
// drain is the caller's loop, bounded by its pass deadline.
func (p *Processor) RevalidateUnresolvedLineage(ctx context.Context, limit int) (revalidated, stillUnknown int, hasMore bool, err error) {
	// One pass at a time: the maintenance pass and the post-sync runner share this
	// processor, and both replace an object's edges as a set.
	if err := p.acquireRevalidation(ctx); err != nil {
		return 0, 0, false, err
	}
	defer p.releaseRevalidation()

	if limit <= 0 {
		limit = revalidationBatch
	}
	pending, err := p.store.FindColumnLineageWithUnknownEndpoint(ctx, ExternalDatasetGUIDPrefix, limit)
	if err != nil {
		return 0, 0, false, errors.Wrap(err, "failed to list lineage with an unknown endpoint")
	}

	affected := affectedRuns(pending)
	revalidated, degraded, stillUnknown, err := p.revalidateRuns(ctx, affected)
	if err != nil {
		return revalidated, stillUnknown, false, err
	}
	if len(pending) > 0 {
		slog.Info("Revalidated ingested lineage whose endpoints the registry did not know",
			"pendingEdges", len(pending),
			"objects", len(affected),
			"revalidatedEdges", revalidated,
			"degradedEdges", degraded,
			"objectsStillUnknown", stillUnknown,
		)
	}
	return revalidated, stillUnknown, len(pending) >= limit && len(affected) > 0, nil
}

// RevalidateContradictedColumnClaims re-checks the ingested edges whose column
// claim the registry has come to contradict: the edge names a column on a
// relation that is a TABLE, and the table has no such column.
//
// These are the edges whose relation was unknown when they were stored, so
// ingestion had to keep the claim unchecked. The sync that made the relation
// known also removed the edge from the unknown-endpoint sweep's reach, so this
// is the only pass that can still fix them. A claim is blanked, the edge stays
// as a table-level dependency, and what is still unknown around it is counted.
//
// It returns the same three values as RevalidateUnresolvedLineage: what the pass
// revalidated, what is still unknown, and whether it filled its batch limit so a
// caller draining a backlog should call again.
//
// limit bounds how many contradicted edges one pass reads; 0 means
// revalidationBatch, the implementation's default. One pass never reads more than
// one batch: the drain is the caller's loop, bounded by its pass deadline.
func (p *Processor) RevalidateContradictedColumnClaims(ctx context.Context, limit int) (revalidated, stillUnknown int, hasMore bool, err error) {
	if err := p.acquireRevalidation(ctx); err != nil {
		return 0, 0, false, err
	}
	defer p.releaseRevalidation()

	if limit <= 0 {
		limit = revalidationBatch
	}
	contradicted, err := p.store.FindColumnLineageWithContradictedColumnClaim(ctx, ExternalDatasetGUIDPrefix, limit)
	if err != nil {
		return 0, 0, false, errors.Wrap(err, "failed to list lineage with a contradicted column claim")
	}

	affected := affectedRuns(contradicted)
	revalidated, degraded, stillUnknown, err := p.revalidateRuns(ctx, affected)
	if err != nil {
		return revalidated, stillUnknown, false, err
	}
	if len(contradicted) > 0 {
		slog.Info("Revalidated ingested lineage whose column claims the registry contradicts",
			"contradictedEdges", len(contradicted),
			"objects", len(affected),
			"revalidatedEdges", revalidated,
			"degradedEdges", degraded,
			"objectsStillUnknown", stillUnknown,
		)
	}
	return revalidated, stillUnknown, len(contradicted) >= limit, nil
}

// affectedRuns returns the ingested runs whose edges a pass has to re-check.
// Edges that other producers own - the analyzers replace their own - are not
// revalidated here.
func affectedRuns(edges []*store.ColumnLineage) map[metaKey]struct{} {
	affected := make(map[metaKey]struct{}, len(edges))
	for _, edge := range edges {
		if edge.MetaType != storepb.MetaType_OPENLINEAGE {
			continue
		}
		affected[metaKey{guid: edge.MetaGUID, metaType: edge.MetaType}] = struct{}{}
	}
	return affected
}

// revalidateRuns replaces the edge set of every affected run: a run's edges are
// stored as a set, so every edge of the run is validated together - replacing
// only the edges a pass was selected by would drop the rest. It returns the
// number of edges kept, the number blanked, and the number of runs still
// referencing a relation the registry does not have.
func (p *Processor) revalidateRuns(ctx context.Context, affected map[metaKey]struct{}) (revalidated, degraded, stillUnknown int, err error) {
	for key := range affected {
		metaGUID, metaType := key.guid, key.metaType
		edges, err := p.store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{
			MetaGUID: &metaGUID,
			MetaType: &metaType,
		})
		if err != nil {
			return revalidated, degraded, stillUnknown, errors.Wrapf(err, "failed to list lineage of %s", metaGUID)
		}
		kept, report, err := p.validateIngestedLineage(ctx, edges)
		if err != nil {
			return revalidated, degraded, stillUnknown, errors.Wrapf(err, "failed to validate lineage of %s", metaGUID)
		}
		if err := p.store.BatchReplaceColumnLineage(ctx, metaGUID, metaType, kept); err != nil {
			return revalidated, degraded, stillUnknown, errors.Wrapf(err, "failed to store revalidated lineage of %s", metaGUID)
		}
		revalidated += len(kept)
		degraded += report.degradedEdges
		if report.unknownRelationEdges > 0 {
			stillUnknown++
		}
	}
	return revalidated, degraded, stillUnknown, nil
}
