package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// ColumnLineage is a single column-level lineage edge.
type ColumnLineage struct {
	ID             int64
	MetaGUID       string
	MetaType       storepb.MetaType
	SourceGUID     string
	SourceColumn   string
	SourceType     storepb.MetaType
	TargetGUID     string
	TargetColumn   string
	TargetType     storepb.MetaType
	RelationType   model.RelationType
	Transformation []model.Transformation
	UpdatedAt      time.Time
}

// FindColumnLineageMessage is the query filter for column lineage.
type FindColumnLineageMessage struct {
	MetaGUID     *string
	MetaType     *storepb.MetaType
	SourceGUID   *string
	SourceColumn *string
	TargetGUID   *string
	TargetColumn *string
	Limit        *int
	Offset       *int
}

// ColumnLineageVersion tracks the analysis state per object.
type ColumnLineageVersion struct {
	MetaGUID string
	MetaType storepb.MetaType
	MetaHash []byte
	// AnalyzerVersion is the lineage.AnalyzerVersion that produced the stored
	// result. A version mismatch re-queues the object, so deploying an analyzer
	// change refreshes lineage without a manual backfill.
	AnalyzerVersion string
	AnalyzedAt      time.Time
	ErrorMessage    *string
}

// ErrAnalysisGone reports that the object an analysis read no longer exists when
// the result is written. The analysis describes SQL the object no longer has, so
// the result is dropped instead of leaving orphan lineage behind.
var ErrAnalysisGone = errors.New("object disappeared during analysis")

// ErrAnalysisStale reports that the object's metadata changed between the
// analysis read and the write. The result was computed from text that is no
// longer current, so the caller drops it and analyzes the new text instead.
var ErrAnalysisStale = errors.New("object metadata changed during analysis")

// ColumnLineageCount is how many distinct objects one object is connected to,
// at table level: the objects its column-level relations mention.
type ColumnLineageCount struct {
	Upstream   int32
	Downstream int32
}

// BatchReplaceColumnLineage deletes any existing lineage edges for the given
// object and inserts the new ones, all within a single transaction.
func (s *Store) BatchReplaceColumnLineage(ctx context.Context, metaGUID string, metaType storepb.MetaType, lineages []*ColumnLineage) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := deleteColumnLineageAt(ctx, tx, metaGUID, metaType); err != nil {
		return err
	}

	if len(lineages) > 0 {
		if err := insertColumnLineage(ctx, tx, metaGUID, metaType, lineages); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// columnLineageInsertStatement inserts one object's edge set. The unique index
// (meta_guid, meta_type, source_guid, source_column, target_guid, target_column,
// relation_type) is the concurrency contract: two writers that replace the same
// object's edges at once (the analyzer and the ingestion revalidation path share a
// process) delete and insert the same rows, and DO NOTHING keeps the later insert
// from duplicating an edge or failing the whole write. transformation is not part
// of the key: it is a fact about the edge, and the same edge has one derivation.
const columnLineageInsertStatement = `
	INSERT INTO column_lineage (meta_guid, meta_type, source_guid, source_column, source_type, target_guid, target_column, target_type, relation_type, transformation)
	SELECT * FROM UNNEST($1::text[], $2::int[], $3::text[], $4::text[], $5::int[], $6::text[], $7::text[], $8::int[], $9::int[], $10::jsonb[])
	ON CONFLICT DO NOTHING
`

func insertColumnLineage(ctx context.Context, tx *sql.Tx, metaGUID string, metaType storepb.MetaType, lineages []*ColumnLineage) error {
	metaGUIDs := make([]string, len(lineages))
	metaTypes := make([]storepb.MetaType, len(lineages))
	sourceGUIDs := make([]string, len(lineages))
	sourceColumns := make([]string, len(lineages))
	sourceTypes := make([]storepb.MetaType, len(lineages))
	targetGUIDs := make([]string, len(lineages))
	targetColumns := make([]string, len(lineages))
	targetTypes := make([]storepb.MetaType, len(lineages))
	relationTypes := make([]int32, len(lineages))
	transformations := make([]string, len(lineages))

	for i, l := range lineages {
		metaGUIDs[i] = metaGUID
		metaTypes[i] = metaType
		sourceGUIDs[i] = l.SourceGUID
		sourceColumns[i] = l.SourceColumn
		sourceTypes[i] = l.SourceType
		targetGUIDs[i] = l.TargetGUID
		targetColumns[i] = l.TargetColumn
		targetTypes[i] = l.TargetType
		relationTypes[i] = int32(l.RelationType)
		b, err := json.Marshal(l.Transformation)
		if err != nil {
			return errors.Wrap(err, "failed to marshal transformation")
		}
		transformations[i] = string(b)
	}

	_, err := tx.ExecContext(ctx, columnLineageInsertStatement,
		pq.Array(metaGUIDs),
		pq.Array(metaTypes),
		pq.Array(sourceGUIDs),
		pq.Array(sourceColumns),
		pq.Array(sourceTypes),
		pq.Array(targetGUIDs),
		pq.Array(targetColumns),
		pq.Array(targetTypes),
		pq.Array(relationTypes),
		pq.Array(transformations),
	)
	return err
}

// ListColumnLineage returns column lineage edges matching the given filter.
// scanColumnLineage reads one column lineage row. Both the filtered listing and
// the revalidation scan select the same columns in the same order.
func scanColumnLineage(rows *sql.Rows) (*ColumnLineage, error) {
	var cl ColumnLineage
	var transformRaw []byte
	if err := rows.Scan(
		&cl.ID,
		&cl.MetaGUID,
		&cl.MetaType,
		&cl.SourceGUID,
		&cl.SourceColumn,
		&cl.SourceType,
		&cl.TargetGUID,
		&cl.TargetColumn,
		&cl.TargetType,
		&cl.RelationType,
		&transformRaw,
		&cl.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(transformRaw) > 0 {
		if err := json.Unmarshal(transformRaw, &cl.Transformation); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal transformation")
		}
	}
	return &cl, nil
}

// FindColumnLineageWithUnknownEndpoint returns the edges whose source or target
// relation the metadata registry has no row for, oldest first. excludePrefix skips
// the GUIDs that are meant to have no row - an external dataset never gets one -
// and limit bounds what one revalidation pass reads.
func (s *Store) FindColumnLineageWithUnknownEndpoint(ctx context.Context, excludePrefix string, limit int) ([]*ColumnLineage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, meta_guid, meta_type, source_guid, source_column, source_type, target_guid, target_column, target_type, relation_type, transformation, updated_at
		FROM column_lineage cl
		WHERE cl.source_guid NOT LIKE $1
		  AND cl.target_guid NOT LIKE $1
		  AND (
		    NOT EXISTS (SELECT 1 FROM meta_registry_resource m WHERE m.guid = cl.source_guid)
		    OR NOT EXISTS (SELECT 1 FROM meta_registry_resource m WHERE m.guid = cl.target_guid)
		  )
		ORDER BY cl.updated_at, cl.id
		LIMIT $2`, excludePrefix+"%", max(limit, 0))
	if err != nil {
		return nil, errors.Wrap(err, "failed to query column lineage with an unknown endpoint")
	}
	defer rows.Close()

	var result []*ColumnLineage
	for rows.Next() {
		cl, err := scanColumnLineage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, cl)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}

// FindColumnLineageWithContradictedColumnClaim returns the ingested edges whose
// column claim the registry now proves wrong: the edge names a column on a
// relation that is a TABLE, and that table has no such column.
//
// Ingestion stores such a claim when the relation is not in the registry yet -
// the table may have been created after the last schema sync - and it cannot
// check it then. Once the relation appears the claim becomes checkable, but the
// edge no longer has an unknown endpoint, so the sweep in
// FindColumnLineageWithUnknownEndpoint can never see it again. This is the query
// that finds it. The claim on a view or materialized view is left alone: those
// keep their columns in their own metadata rather than as registry rows.
func (s *Store) FindColumnLineageWithContradictedColumnClaim(ctx context.Context, excludePrefix string, limit int) ([]*ColumnLineage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, meta_guid, meta_type, source_guid, source_column, source_type, target_guid, target_column, target_type, relation_type, transformation, updated_at
		FROM column_lineage cl
		WHERE cl.meta_type = $1
		  AND cl.source_guid NOT LIKE $2
		  AND cl.target_guid NOT LIKE $2
		  AND (
		    (cl.target_column <> ''
		      AND EXISTS (SELECT 1 FROM meta_registry_resource t WHERE t.guid = cl.target_guid AND t.object_type = $3)
		      AND NOT EXISTS (SELECT 1 FROM meta_registry_resource c WHERE c.guid = cl.target_guid || ';' || cl.target_column AND c.object_type = $4))
		    OR
		    (cl.source_column <> ''
		      AND EXISTS (SELECT 1 FROM meta_registry_resource s WHERE s.guid = cl.source_guid AND s.object_type = $3)
		      AND NOT EXISTS (SELECT 1 FROM meta_registry_resource c WHERE c.guid = cl.source_guid || ';' || cl.source_column AND c.object_type = $4))
		  )
		ORDER BY cl.updated_at, cl.id
		LIMIT $5`,
		storepb.MetaType_OPENLINEAGE,
		excludePrefix+"%",
		storepb.MetaType_TABLE,
		storepb.MetaType_COLUMN,
		max(limit, 0))
	if err != nil {
		return nil, errors.Wrap(err, "failed to query column lineage with a contradicted column claim")
	}
	defer rows.Close()

	var result []*ColumnLineage
	for rows.Next() {
		cl, err := scanColumnLineage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, cl)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "rows iteration error")
	}
	return result, nil
}

func (s *Store) ListColumnLineage(ctx context.Context, find *FindColumnLineageMessage) ([]*ColumnLineage, error) {
	where, args := []string{"TRUE"}, []any{}

	if v := find.MetaGUID; v != nil {
		where = append(where, fmt.Sprintf("meta_guid = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.MetaType; v != nil {
		where = append(where, fmt.Sprintf("meta_type = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.SourceGUID; v != nil {
		where = append(where, fmt.Sprintf("source_guid = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.SourceColumn; v != nil {
		where = append(where, fmt.Sprintf("source_column = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.TargetColumn; v != nil {
		where = append(where, fmt.Sprintf("target_column = $%d", len(args)+1))
		args = append(args, *v)
	}
	if v := find.TargetGUID; v != nil {
		where = append(where, fmt.Sprintf("target_guid = $%d", len(args)+1))
		args = append(args, *v)
	}

	query := fmt.Sprintf(`
		SELECT id, meta_guid, meta_type, source_guid, source_column, source_type, target_guid, target_column, target_type, relation_type, transformation, updated_at
		FROM column_lineage
		WHERE %s
		ORDER BY id`, strings.Join(where, " AND "))

	if v := find.Limit; v != nil {
		args = append(args, max(*v, 0))
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if v := find.Offset; v != nil {
		args = append(args, max(*v, 0))
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*ColumnLineage
	for rows.Next() {
		cl, err := scanColumnLineage(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, cl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// CountColumnLineageByGUID returns the degree of every given object. Two
// aggregates answer the whole batch: the incoming edges grouped by their target
// give the upstream counts and the outgoing edges grouped by their source give
// the downstream ones. The counts are of distinct objects, not of edges, so a
// join on five columns counts once. A guid with no edges is absent from the
// result rather than reported as zero.
func (s *Store) CountColumnLineageByGUID(ctx context.Context, guids []string) (map[string]*ColumnLineageCount, error) {
	counts := make(map[string]*ColumnLineageCount, len(guids))
	if len(guids) == 0 {
		return counts, nil
	}

	upstream, err := s.countColumnLineageBy(ctx, `
		SELECT target_guid, COUNT(DISTINCT source_guid)
		FROM column_lineage
		WHERE target_guid = ANY($1)
		GROUP BY target_guid`, guids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to count upstream lineage")
	}
	for guid, count := range upstream {
		counts[guid] = &ColumnLineageCount{Upstream: count}
	}

	downstream, err := s.countColumnLineageBy(ctx, `
		SELECT source_guid, COUNT(DISTINCT target_guid)
		FROM column_lineage
		WHERE source_guid = ANY($1)
		GROUP BY source_guid`, guids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to count downstream lineage")
	}
	for guid, count := range downstream {
		if existing, ok := counts[guid]; ok {
			existing.Downstream = count
			continue
		}
		counts[guid] = &ColumnLineageCount{Downstream: count}
	}

	return counts, nil
}

// countColumnLineageBy runs one grouped count and keys the result by the guid
// the query groups on.
func (s *Store) countColumnLineageBy(ctx context.Context, query string, guids []string) (map[string]int32, error) {
	rows, err := s.GetDB().QueryContext(ctx, query, pq.Array(guids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int32, len(guids))
	for rows.Next() {
		var guid string
		var count int32
		if err := rows.Scan(&guid, &count); err != nil {
			return nil, err
		}
		counts[guid] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

// columnLineageEndpointGUIDQuery selects the objects that are an endpoint of at
// least one column-level edge, i.e. an edge that names a column on at least one
// side. A table-level edge names no column at all and is deliberately not an
// answer: the pages this feeds advertise column lineage, and a table-level
// dependency gives a column page nothing to show.
const columnLineageEndpointGUIDQuery = `
	SELECT target_guid FROM column_lineage
	WHERE target_guid = ANY($1) AND (source_column <> '' OR target_column <> '')
	UNION
	SELECT source_guid FROM column_lineage
	WHERE source_guid = ANY($1) AND (source_column <> '' OR target_column <> '')`

// maxColumnLineageProbeGuids bounds the GUIDs one endpoint probe binds. A list
// request may ask about the whole dataset window, and binding thousands of GUIDs
// in one statement makes the planner scan the table instead of probing the
// endpoint indexes; a batch keeps every query selective.
const maxColumnLineageProbeGuids = 1000

// ListColumnLineageEndpoints reports which of the given objects are an endpoint
// of at least one column-level edge, i.e. an edge that names a column on at
// least one side. A table-level edge names no column at all and is deliberately
// not an answer: the pages this feeds advertise column lineage, and a table-level
// dependency gives a column page nothing to show. Each branch probes the endpoint
// index (`idx_column_lineage_target` / `idx_column_lineage_source`), and a batch
// of up to maxColumnLineageProbeGuids costs one index scan per direction. An
// object with no edge is absent from the result rather than reported as false.
func (s *Store) ListColumnLineageEndpoints(ctx context.Context, guids []string) (map[string]struct{}, error) {
	found := make(map[string]struct{}, len(guids))
	for start := 0; start < len(guids); start += maxColumnLineageProbeGuids {
		end := min(start+maxColumnLineageProbeGuids, len(guids))
		if err := s.probeColumnLineageEndpoints(ctx, guids[start:end], found); err != nil {
			return nil, err
		}
	}
	return found, nil
}

// probeColumnLineageEndpoints runs one endpoint probe and adds its answer to
// found.
func (s *Store) probeColumnLineageEndpoints(ctx context.Context, guids []string, found map[string]struct{}) error {
	rows, err := s.GetDB().QueryContext(ctx, columnLineageEndpointGUIDQuery, pq.Array(guids))
	if err != nil {
		return errors.Wrap(err, "failed to query column lineage endpoints")
	}
	defer rows.Close()

	for rows.Next() {
		var guid string
		if err := rows.Scan(&guid); err != nil {
			return errors.Wrap(err, "failed to scan a column lineage endpoint")
		}
		found[guid] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "failed to read column lineage endpoints")
	}
	return nil
}

// columnLineageColumnQuery selects the columns an object's column-level edges
// name on the object's own side: the target columns of its incoming edges and
// the source columns of its outgoing ones. The object is the target of what
// writes it and the source of what it writes, so both sides describe its fields.
const columnLineageColumnQuery = `
	SELECT column_name FROM (
		SELECT target_column AS column_name FROM column_lineage
		WHERE target_guid = $1 AND target_column <> ''
		UNION
		SELECT source_column FROM column_lineage
		WHERE source_guid = $1 AND source_column <> ''
	) named
	ORDER BY column_name
	LIMIT $2`

// ListColumnLineageColumnsByGUID returns the fields an object's column-level
// edges name on its own side, sorted and bounded. It is what marks a schema
// field as column-lineage ready: the same edges the column-lineage page draws
// are the ones that make a field participate.
func (s *Store) ListColumnLineageColumnsByGUID(ctx context.Context, guid string) ([]string, error) {
	rows, err := s.GetDB().QueryContext(ctx, columnLineageColumnQuery, guid, maxOpenLineageDatasetColumnFields)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the column lineage columns")
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, errors.Wrap(err, "failed to scan a column lineage column")
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read the column lineage columns")
	}
	return columns, nil
}

// ReplaceColumnLineageIfCurrent replaces an object's edges only when its stored
// metadata still matches the hash the analysis read. The edge replacement, the
// version row and the gap message commit in one transaction, and the registry row
// stays shared-locked until then: a concurrent schema-sync delete or update waits,
// and a result computed from text that is already gone is dropped instead of
// re-creating lineage for a deleted object.
func (s *Store) ReplaceColumnLineageIfCurrent(ctx context.Context, metaGUID string, metaType storepb.MetaType, expectedMetaHash []byte, analyzerVersion string, lineages []*ColumnLineage, errorMessage *string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := lockAnalysisState(ctx, tx, metaGUID, metaType, expectedMetaHash); err != nil {
		return err
	}
	if err := deleteColumnLineageAt(ctx, tx, metaGUID, metaType); err != nil {
		return err
	}
	if len(lineages) > 0 {
		if err := insertColumnLineage(ctx, tx, metaGUID, metaType, lineages); err != nil {
			return err
		}
	}
	err = upsertColumnLineageVersionAt(ctx, tx, &ColumnLineageVersion{
		MetaGUID:        metaGUID,
		MetaType:        metaType,
		MetaHash:        expectedMetaHash,
		AnalyzerVersion: analyzerVersion,
		ErrorMessage:    errorMessage,
	})
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MarkAnalyzedIfCurrent records a deliberate skip (an engine without an analyzer)
// at the hash that was analyzed. It touches no edges: the skip says "this build
// cannot analyze the object", not "the lineage is wrong". An object whose
// definition became empty takes MarkAnalysisFailedIfCurrent instead, because the
// edges of the SQL it no longer has are wrong.
func (s *Store) MarkAnalyzedIfCurrent(ctx context.Context, metaGUID string, metaType storepb.MetaType, expectedMetaHash []byte, analyzerVersion string, errorMessage *string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := lockAnalysisState(ctx, tx, metaGUID, metaType, expectedMetaHash); err != nil {
		return err
	}
	err = upsertColumnLineageVersionAt(ctx, tx, &ColumnLineageVersion{
		MetaGUID:        metaGUID,
		MetaType:        metaType,
		MetaHash:        expectedMetaHash,
		AnalyzerVersion: analyzerVersion,
		ErrorMessage:    errorMessage,
	})
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MarkAnalysisFailedIfCurrent clears an object's edges and records the parse
// failure in one transaction. Clearing the edges is what keeps lineage from a
// previous definition on screen; the version row carries the current hash so the
// hourly scan stops retrying a definition that cannot be parsed.
func (s *Store) MarkAnalysisFailedIfCurrent(ctx context.Context, metaGUID string, metaType storepb.MetaType, expectedMetaHash []byte, analyzerVersion, errorMessage string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := lockAnalysisState(ctx, tx, metaGUID, metaType, expectedMetaHash); err != nil {
		return err
	}
	if err := deleteColumnLineageAt(ctx, tx, metaGUID, metaType); err != nil {
		return err
	}
	err = upsertColumnLineageVersionAt(ctx, tx, &ColumnLineageVersion{
		MetaGUID:        metaGUID,
		MetaType:        metaType,
		MetaHash:        expectedMetaHash,
		AnalyzerVersion: analyzerVersion,
		ErrorMessage:    &errorMessage,
	})
	if err != nil {
		return err
	}
	return tx.Commit()
}

// deleteColumnLineageAt removes an object's edges inside the caller's transaction.
func deleteColumnLineageAt(ctx context.Context, tx *sql.Tx, metaGUID string, metaType storepb.MetaType) error {
	_, err := tx.ExecContext(ctx,
		`DELETE FROM column_lineage WHERE meta_guid = $1 AND meta_type = $2`,
		metaGUID, metaType,
	)
	return err
}

// columnLineageDeleteByEndpointStatement deletes every edge that names one of the given
// objects as a source or target. GUIDs are matched rather than (guid, type) pairs so each
// predicate is a bitmap index scan on the leading column of a three-index OR; a GUID from a
// schema snapshot names one object in the managed namespace, so the type adds nothing.
const columnLineageDeleteByEndpointStatement = `
	DELETE FROM column_lineage
	WHERE source_guid = ANY($1::text[])
		OR target_guid = ANY($1::text[])
`

// columnLineageDeleteByProducerStatement deletes the edges an analyzed object produced.
const columnLineageDeleteByProducerStatement = `
	DELETE FROM column_lineage WHERE meta_guid = ANY($1::text[])
`

const columnLineageVersionDeleteByProducerStatement = `
	DELETE FROM column_lineage_version WHERE meta_guid = ANY($1::text[])
`

// lineageEndpointTypes are the object types that can appear as an edge's source or target.
// Every other schema-sync-managed type is never a lineage endpoint, so its deletion must not
// probe the lineage indexes at all.
var lineageEndpointTypes = []storepb.MetaType{
	storepb.MetaType_TABLE,
	storepb.MetaType_VIEW,
	storepb.MetaType_MATERIALIZED_VIEW,
	storepb.MetaType_EXTERNAL_TABLE,
}

// lineageProducerTypes are the object types the analyzers produce edges for, keyed by
// meta_guid. A table is never analyzed, a view and a materialized view are.
var lineageProducerTypes = []storepb.MetaType{
	storepb.MetaType_VIEW,
	storepb.MetaType_MATERIALIZED_VIEW,
}

// splitLineageDeletions returns the GUIDs that can be an edge endpoint and the GUIDs that can
// own an edge, dropping every deleted object that can be neither.
func splitLineageDeletions(keys []MetaGUIDKey) (endpoints, producers []string) {
	for _, key := range keys {
		if slices.Contains(lineageEndpointTypes, key.ObjectType) {
			endpoints = append(endpoints, key.GUID)
		}
		if slices.Contains(lineageProducerTypes, key.ObjectType) {
			producers = append(producers, key.GUID)
		}
	}
	return endpoints, producers
}

// DeleteColumnLineageByMetaGUIDs removes the lineage that mentions any of the given objects
// and the analysis state of the analyzed ones, in a fixed number of statements. A destructive
// schema sync deletes a database's objects at once — most of them columns, which are never
// lineage endpoints — so the cleanup filters by type and probes the indexes once per
// direction instead of once per deleted object, and does so inside the write transaction.
func (*Store) DeleteColumnLineageByMetaGUIDs(ctx context.Context, tx *sql.Tx, keys []MetaGUIDKey) error {
	endpoints, producers := splitLineageDeletions(keys)
	if len(endpoints) > 0 {
		if _, err := tx.ExecContext(ctx, columnLineageDeleteByEndpointStatement, pq.Array(endpoints)); err != nil {
			return errors.Wrap(err, "failed to delete the column lineage of deleted objects")
		}
	}
	if len(producers) > 0 {
		if _, err := tx.ExecContext(ctx, columnLineageDeleteByProducerStatement, pq.Array(producers)); err != nil {
			return errors.Wrap(err, "failed to delete the column lineage the deleted objects produced")
		}
		if _, err := tx.ExecContext(ctx, columnLineageVersionDeleteByProducerStatement, pq.Array(producers)); err != nil {
			return errors.Wrap(err, "failed to delete the analysis state of deleted objects")
		}
	}
	return nil
}

// lockAnalysisState reads the object's current meta hash under a shared row lock
// and fails with ErrAnalysisGone/ErrAnalysisStale when the result no longer
// describes the stored object. The lock is held until the transaction ends, so a
// concurrent schema-sync update or delete serializes against this write.
func lockAnalysisState(ctx context.Context, tx *sql.Tx, metaGUID string, metaType storepb.MetaType, expectedMetaHash []byte) error {
	var current []byte
	err := tx.QueryRowContext(ctx, `
		SELECT meta_hash FROM meta_registry_resource
		WHERE guid = $1 AND object_type = $2
		FOR SHARE
	`, metaGUID, metaType).Scan(&current)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAnalysisGone
		}
		return errors.Wrapf(err, "failed to lock the analysis state of %q", metaGUID)
	}
	if !bytes.Equal(current, expectedMetaHash) {
		return ErrAnalysisStale
	}
	return nil
}

// upsertColumnLineageVersionAt writes an analysis-state row inside the caller's
// transaction, so it commits exactly when the edges it describes commit.
func upsertColumnLineageVersionAt(ctx context.Context, tx *sql.Tx, v *ColumnLineageVersion) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO column_lineage_version (meta_guid, meta_type, meta_hash, analyzer_version, analyzed_at, error_message)
		VALUES ($1, $2, $3, $4, NOW(), $5)
		ON CONFLICT (meta_guid, meta_type) DO UPDATE SET
			meta_hash = EXCLUDED.meta_hash,
			analyzer_version = EXCLUDED.analyzer_version,
			analyzed_at = EXCLUDED.analyzed_at,
			error_message = EXCLUDED.error_message
	`, v.MetaGUID, v.MetaType, v.MetaHash, v.AnalyzerVersion, v.ErrorMessage)
	return err
}

// RecordAnalysisError updates the error message of an existing analysis-state row
// without touching its hash: a transient failure must not erase the evidence that
// the object was analyzed, and the next full scan still sees the last successful
// result as current. A row that does not exist is left alone; the caller retries,
// and the failure is visible in the log either way.
func (s *Store) RecordAnalysisError(ctx context.Context, metaGUID string, metaType storepb.MetaType, message string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE column_lineage_version
		SET analyzed_at = NOW(), error_message = $3
		WHERE meta_guid = $1 AND meta_type = $2
	`, metaGUID, metaType, message)
	return err
}

// ListColumnLineageVersions returns the analysis state of every object of the
// given meta type, keyed by meta GUID. The analyzer used to issue one lookup per
// object on every scan; column_lineage_version holds one row per analyzable
// object, so a single query is enough.
func (s *Store) ListColumnLineageVersions(ctx context.Context, metaType storepb.MetaType) (map[string]*ColumnLineageVersion, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT meta_guid, meta_type, meta_hash, analyzer_version, analyzed_at, error_message
		FROM column_lineage_version
		WHERE meta_type = $1
	`, metaType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*ColumnLineageVersion)
	for rows.Next() {
		var v ColumnLineageVersion
		if err := rows.Scan(&v.MetaGUID, &v.MetaType, &v.MetaHash, &v.AnalyzerVersion, &v.AnalyzedAt, &v.ErrorMessage); err != nil {
			return nil, err
		}
		result[v.MetaGUID] = &v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetColumnLineageVersion returns the analysis version record for an object, or nil if not found.
func (s *Store) GetColumnLineageVersion(ctx context.Context, metaGUID string, metaType storepb.MetaType) (*ColumnLineageVersion, error) {
	row := s.GetDB().QueryRowContext(ctx, `
		SELECT meta_guid, meta_type, meta_hash, analyzer_version, analyzed_at, error_message
		FROM column_lineage_version
		WHERE meta_guid = $1 AND meta_type = $2
	`, metaGUID, metaType)

	var v ColumnLineageVersion
	err := row.Scan(&v.MetaGUID, &v.MetaType, &v.MetaHash, &v.AnalyzerVersion, &v.AnalyzedAt, &v.ErrorMessage)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}
