package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// The kinds of openlineage_dataset_member. A task row counts the runs of one job
// that reference a dataset in one direction; an integration or source row counts
// the references that reported that value. The discriminator is what lets one
// table carry both the job counts and the arrays the list shows.
const (
	openLineageDatasetMemberInputTask   = "task:input"
	openLineageDatasetMemberOutputTask  = "task:output"
	openLineageDatasetMemberIntegration = "integration"
	openLineageDatasetMemberSource      = "source"
)

// openLineageDatasetMemberKey identifies one member row of a dataset.
type openLineageDatasetMemberKey struct {
	kind  string
	value string
}

// openLineageDatasetDelta is how one ingest transaction changes one dataset's
// aggregate. The counts move by these deltas rather than being recomputed from
// the dataset's references, which is what kept the read path off the reference
// table: a dataset's history can be millions of rows, and folding one run into
// its aggregate costs the references of that run.
type openLineageDatasetDelta struct {
	namespace             string
	name                  string
	refDelta              int
	columnLineageRefDelta int
	members               map[openLineageDatasetMemberKey]int
	// lastSeen is the newest event time among the references that were added;
	// removed reports whether any reference went away, in which case the newest
	// remaining one has to be read back instead.
	lastSeen *time.Time
	removed  bool
}

// openLineageRunDatasetReplacement is one run's references before and after an
// ingest replaced them.
type openLineageRunDatasetReplacement struct {
	Old []*OpenLineageRunDatasetMessage
	New []*OpenLineageRunDatasetMessage
}

// datasetRefKey is the identity of one reference as far as the aggregate is
// concerned: two references that agree on every field below contribute exactly
// the same thing, so their difference is the delta.
type datasetRefKey struct {
	namespace        string
	name             string
	direction        string
	taskGUID         string
	hasColumnLineage bool
	integration      string
	source           string
	eventTime        int64
	hasEventTime     bool
}

// datasetKey is the identity of one dataset.
type datasetKey struct {
	namespace string
	name      string
}

// openLineageDatasetDeltas folds a transaction's run replacements into one delta
// per dataset they touched. References that the replacement kept as they were
// cancel out, so a redelivery that changes nothing writes nothing; a reference
// that went away is reported by a negative count, which is what makes the
// aggregate exact rather than monotone.
func openLineageDatasetDeltas(replacements []openLineageRunDatasetReplacement) []*openLineageDatasetDelta {
	counts := make(map[datasetKey]map[datasetRefKey]int)
	add := func(ref *OpenLineageRunDatasetMessage, count int) {
		key := datasetKey{namespace: ref.Namespace, name: ref.Name}
		refs := counts[key]
		if refs == nil {
			refs = make(map[datasetRefKey]int)
			counts[key] = refs
		}
		refs[datasetRefKeyOf(ref)] += count
	}
	for _, replacement := range replacements {
		for _, ref := range replacement.Old {
			add(ref, -1)
		}
		for _, ref := range replacement.New {
			add(ref, 1)
		}
	}

	deltas := make([]*openLineageDatasetDelta, 0, len(counts))
	for dataset, refs := range counts {
		delta := &openLineageDatasetDelta{
			namespace: dataset.namespace,
			name:      dataset.name,
			members:   make(map[openLineageDatasetMemberKey]int),
		}
		for ref, count := range refs {
			if count == 0 {
				continue
			}
			delta.refDelta += count
			if ref.hasColumnLineage {
				delta.columnLineageRefDelta += count
			}
			delta.addMember(openLineageDatasetMemberKey{kind: taskMemberKind(ref.direction), value: ref.taskGUID}, count)
			if ref.integration != "" {
				delta.addMember(openLineageDatasetMemberKey{kind: openLineageDatasetMemberIntegration, value: ref.integration}, count)
			}
			if ref.source != "" {
				delta.addMember(openLineageDatasetMemberKey{kind: openLineageDatasetMemberSource, value: ref.source}, count)
			}
			if count < 0 {
				// A reference this dataset had is gone. The newest remaining
				// reference has to be read back, because the one that went away
				// may have been it.
				delta.removed = true
				continue
			}
			if ref.hasEventTime {
				eventTime := time.Unix(0, ref.eventTime).UTC()
				if delta.lastSeen == nil || eventTime.After(*delta.lastSeen) {
					delta.lastSeen = &eventTime
				}
			}
		}
		// A delta that would write nothing is dropped, but "no reference, no job and
		// no value moved" is not the same as "nothing to write": a replacement that
		// only moved a reference's event time cancels out and still has to take the
		// newest event time of the dataset with it.
		if delta.refDelta == 0 && delta.columnLineageRefDelta == 0 && len(delta.members) == 0 && !delta.removed && delta.lastSeen == nil {
			continue
		}
		deltas = append(deltas, delta)
	}

	slices.SortFunc(deltas, compareOpenLineageDatasetDeltas)
	return deltas
}

// addMember nets one member's contribution into the delta.
func (d *openLineageDatasetDelta) addMember(key openLineageDatasetMemberKey, count int) {
	if total := d.members[key] + count; total != 0 {
		d.members[key] = total
		return
	}
	delete(d.members, key)
}

// taskMemberKind maps a reference direction onto the member kind that counts the
// jobs referencing the dataset that way.
func taskMemberKind(direction string) string {
	if direction == OpenLineageDatasetDirectionOutput {
		return openLineageDatasetMemberOutputTask
	}
	return openLineageDatasetMemberInputTask
}

func datasetRefKeyOf(ref *OpenLineageRunDatasetMessage) datasetRefKey {
	key := datasetRefKey{
		namespace:        ref.Namespace,
		name:             ref.Name,
		direction:        ref.Direction,
		taskGUID:         ref.TaskGUID,
		hasColumnLineage: ref.HasColumnLineage,
		integration:      ref.Integration,
		source:           ref.Source,
	}
	if ref.EventTime != nil {
		key.eventTime = ref.EventTime.UnixNano()
		key.hasEventTime = true
	}
	return key
}

// compareOpenLineageDatasetDeltas orders one transaction's deltas so the dataset
// rows are always locked in the same order.
func compareOpenLineageDatasetDeltas(a, b *openLineageDatasetDelta) int {
	if diff := strings.Compare(a.namespace, b.namespace); diff != 0 {
		return diff
	}
	return strings.Compare(a.name, b.name)
}

// valuesList renders a SQL VALUES list whose rows all have the same shape: one
// cast per column, empty where the target column already types the parameter. A
// VALUES list in a FROM clause gets no types from a target column, so its casts are
// what tell PostgreSQL what it is comparing and assigning. The placeholders are
// numbered across the whole list, which is what a statement that binds more than
// one list has to get right.
type valuesList struct {
	casts    []string
	args     []any
	rowCount int
}

func newValuesList(casts ...string) *valuesList {
	return &valuesList{casts: casts}
}

// add appends one row. A row of the wrong width is a programming error rather than
// a runtime condition, so it fails loudly instead of rendering broken SQL.
func (l *valuesList) add(values ...any) {
	if len(values) != len(l.casts) {
		panic(fmt.Sprintf("values list row of %d values for a list %d wide", len(values), len(l.casts)))
	}
	l.args = append(l.args, values...)
	l.rowCount++
}

func (l *valuesList) count() int {
	return l.rowCount
}

func (l *valuesList) String() string {
	rows := make([]string, 0, l.rowCount)
	placeholders := make([]string, len(l.casts))
	for row := 0; row < l.rowCount; row++ {
		for column, cast := range l.casts {
			placeholders[column] = fmt.Sprintf("$%d", row*len(l.casts)+column+1)
			if cast != "" {
				placeholders[column] += "::" + cast
			}
		}
		rows = append(rows, "("+strings.Join(placeholders, ", ")+")")
	}
	return strings.Join(rows, ", ")
}

// maxOpenLineageDatasetAggregateRows bounds the rows one batched aggregate
// statement carries. A statement's parameter count is limited and one ingest batch
// can name an unbounded number of datasets, so the maintenance is chunked rather
// than sent as one statement per dataset or as a single statement for the batch.
const maxOpenLineageDatasetAggregateRows = 1000

// openLineageDatasetState is a dataset's stored aggregate, read while its row is
// locked for one ingest transaction.
type openLineageDatasetState struct {
	RefCount              int64
	SourceJobCount        int32
	TargetJobCount        int32
	ColumnLineageRefCount int64
	LastSeen              *time.Time
}

// openLineageDatasetJobDelta is how many jobs one dataset gained or lost per
// direction, which is what a task member row appearing or disappearing means.
type openLineageDatasetJobDelta struct {
	source int32
	target int32
}

// openLineageDatasetMemberRef identifies one member row of one dataset.
type openLineageDatasetMemberRef struct {
	dataset datasetKey
	kind    string
	value   string
}

// openLineageDatasetMemberDelta is one member's net change in a transaction.
type openLineageDatasetMemberDelta struct {
	dataset datasetKey
	member  openLineageDatasetMemberKey
	delta   int
}

// applyOpenLineageDatasetDeltas folds one ingest transaction's run replacements
// into the per-dataset aggregate. It runs after the references are written and
// after every task lock of the transaction has been taken. Every dataset row is
// locked in (namespace, name) order before anything under it is touched, which
// keeps a batch's lock order global — tasks ascending, then datasets ascending — so
// two batches cannot deadlock on each other's datasets. The work is then batched
// per statement class, so an event that names a thousand datasets costs a handful
// of round trips rather than thousands.
func applyOpenLineageDatasetDeltas(ctx context.Context, tx *sql.Tx, replacements []openLineageRunDatasetReplacement) error {
	deltas := openLineageDatasetDeltas(replacements)
	if len(deltas) == 0 {
		return nil
	}

	states, err := lockOpenLineageDatasets(ctx, tx, deltas)
	if err != nil {
		return err
	}
	jobDeltas, err := applyOpenLineageDatasetMemberDeltas(ctx, tx, deltas)
	if err != nil {
		return err
	}
	return updateOpenLineageDatasetAggregates(ctx, tx, deltas, states, jobDeltas)
}

// lockOpenLineageDatasets takes every dataset's row lock and returns its stored
// aggregate. The insert makes the rows exist for the member rows that follow, and
// the no-op update is what locks a row that is already there; the lock is what
// makes the counters exact, since two writers of one dataset then serialize.
func lockOpenLineageDatasets(ctx context.Context, tx *sql.Tx, deltas []*openLineageDatasetDelta) (map[datasetKey]*openLineageDatasetState, error) {
	states := make(map[datasetKey]*openLineageDatasetState, len(deltas))
	for start := 0; start < len(deltas); start += maxOpenLineageDatasetAggregateRows {
		end := min(start+maxOpenLineageDatasetAggregateRows, len(deltas))
		values := newValuesList("", "")
		for _, delta := range deltas[start:end] {
			values.add(delta.namespace, delta.name)
		}

		rows, err := tx.QueryContext(ctx, `
			INSERT INTO openlineage_dataset (namespace, name)
			VALUES `+values.String()+`
			ON CONFLICT (namespace, name) DO UPDATE SET updated_at = openlineage_dataset.updated_at
			RETURNING namespace, name, ref_count, source_job_count, target_job_count, column_lineage_ref_count, last_seen
		`, values.args...)
		if err != nil {
			return nil, errors.Wrap(err, "failed to lock openlineage datasets")
		}
		if err := scanOpenLineageDatasetStates(rows, states); err != nil {
			return nil, err
		}
	}
	return states, nil
}

func scanOpenLineageDatasetStates(rows *sql.Rows, states map[datasetKey]*openLineageDatasetState) error {
	defer rows.Close()

	for rows.Next() {
		var key datasetKey
		var state openLineageDatasetState
		var lastSeen sql.NullTime
		if err := rows.Scan(
			&key.namespace,
			&key.name,
			&state.RefCount,
			&state.SourceJobCount,
			&state.TargetJobCount,
			&state.ColumnLineageRefCount,
			&lastSeen,
		); err != nil {
			return errors.Wrap(err, "failed to scan an openlineage dataset state")
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			state.LastSeen = &t
		}
		states[key] = &state
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "failed to read openlineage dataset states")
	}
	return nil
}

// applyOpenLineageDatasetMemberDeltas moves every member's reference count and
// reports how each dataset's job counts moved. A member that reaches zero has lost
// its last reference and is deleted, which is what takes a job, an integration or a
// source back out of the aggregate.
func applyOpenLineageDatasetMemberDeltas(ctx context.Context, tx *sql.Tx, deltas []*openLineageDatasetDelta) (map[datasetKey]*openLineageDatasetJobDelta, error) {
	members := openLineageDatasetMemberDeltaList(deltas)
	jobDeltas := make(map[datasetKey]*openLineageDatasetJobDelta)

	for start := 0; start < len(members); start += maxOpenLineageDatasetAggregateRows {
		end := min(start+maxOpenLineageDatasetAggregateRows, len(members))
		chunk := members[start:end]

		// An INSERT takes the parameter types from its target columns; the two
		// statements below read a VALUES list and have to be told.
		added, removed := newValuesList("", "", "", "", ""), newValuesList("text", "text", "text", "text", "bigint")
		expected := make(map[openLineageDatasetMemberRef]int, len(chunk))
		for _, delta := range chunk {
			if delta.delta > 0 {
				added.add(delta.dataset.namespace, delta.dataset.name, delta.member.kind, delta.member.value, delta.delta)
				expected[openLineageDatasetMemberRef{dataset: delta.dataset, kind: delta.member.kind, value: delta.member.value}] = delta.delta
				continue
			}
			removed.add(delta.dataset.namespace, delta.dataset.name, delta.member.kind, delta.member.value, delta.delta)
		}

		if added.count() > 0 {
			if err := addOpenLineageDatasetMembers(ctx, tx, added, expected, jobDeltas); err != nil {
				return nil, err
			}
		}
		if removed.count() > 0 {
			if err := removeOpenLineageDatasetMembers(ctx, tx, removed, jobDeltas); err != nil {
				return nil, err
			}
		}
	}
	return jobDeltas, nil
}

func openLineageDatasetMemberDeltaList(deltas []*openLineageDatasetDelta) []openLineageDatasetMemberDelta {
	var members []openLineageDatasetMemberDelta
	for _, delta := range deltas {
		dataset := datasetKey{namespace: delta.namespace, name: delta.name}
		for _, member := range sortedOpenLineageDatasetMemberKeys(delta.members) {
			members = append(members, openLineageDatasetMemberDelta{dataset: dataset, member: member, delta: delta.members[member]})
		}
	}
	return members
}

// addOpenLineageDatasetMembers raises the reference counts of the members that
// gained references, and counts the datasets that gained a job. A member row never
// holds a zero count, so a result equal to the delta is the row this insert created.
func addOpenLineageDatasetMembers(ctx context.Context, tx *sql.Tx, added *valuesList, expected map[openLineageDatasetMemberRef]int, jobDeltas map[datasetKey]*openLineageDatasetJobDelta) error {
	rows, err := tx.QueryContext(ctx, `
		INSERT INTO openlineage_dataset_member (namespace, name, kind, value, ref_count)
		VALUES `+added.String()+`
		ON CONFLICT (namespace, name, kind, value)
		DO UPDATE SET ref_count = openlineage_dataset_member.ref_count + EXCLUDED.ref_count
		RETURNING namespace, name, kind, value, ref_count
	`, added.args...)
	if err != nil {
		return errors.Wrap(err, "failed to add openlineage dataset members")
	}
	defer rows.Close()

	for rows.Next() {
		var ref openLineageDatasetMemberRef
		var refCount int64
		if err := rows.Scan(&ref.dataset.namespace, &ref.dataset.name, &ref.kind, &ref.value, &refCount); err != nil {
			return errors.Wrap(err, "failed to scan an openlineage dataset member")
		}
		if refCount == int64(expected[ref]) {
			countOpenLineageDatasetJob(jobDeltas, ref.dataset, ref.kind, 1)
		}
	}
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "failed to add openlineage dataset members")
	}
	return nil
}

// removeOpenLineageDatasetMembers lowers the reference counts of the members that
// lost references: a member that reaches zero lost its last reference, so its row
// goes and its dataset loses that job or value.
func removeOpenLineageDatasetMembers(ctx context.Context, tx *sql.Tx, removed *valuesList, jobDeltas map[datasetKey]*openLineageDatasetJobDelta) error {
	rows, err := tx.QueryContext(ctx, `
		UPDATE openlineage_dataset_member member SET ref_count = member.ref_count + delta.ref_count
		FROM (VALUES `+removed.String()+`) AS delta(namespace, name, kind, value, ref_count)
		WHERE member.namespace = delta.namespace AND member.name = delta.name
			AND member.kind = delta.kind AND member.value = delta.value
		RETURNING member.namespace, member.name, member.kind, member.value, member.ref_count
	`, removed.args...)
	if err != nil {
		return errors.Wrap(err, "failed to remove openlineage dataset members")
	}

	// The rows have to be drained and closed before the next statement runs on this
	// transaction, so the delete below waits for the scan to finish.
	exhausted := newValuesList("text", "text", "text", "text")
	if err := func() error {
		defer rows.Close()
		for rows.Next() {
			var ref openLineageDatasetMemberRef
			var refCount int64
			if err := rows.Scan(&ref.dataset.namespace, &ref.dataset.name, &ref.kind, &ref.value, &refCount); err != nil {
				return errors.Wrap(err, "failed to scan an openlineage dataset member")
			}
			if refCount > 0 {
				continue
			}
			countOpenLineageDatasetJob(jobDeltas, ref.dataset, ref.kind, -1)
			exhausted.add(ref.dataset.namespace, ref.dataset.name, ref.kind, ref.value)
		}
		if err := rows.Err(); err != nil {
			return errors.Wrap(err, "failed to read openlineage dataset members")
		}
		return nil
	}(); err != nil {
		return err
	}
	if exhausted.count() == 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM openlineage_dataset_member member
		USING (VALUES `+exhausted.String()+`) AS expired(namespace, name, kind, value)
		WHERE member.namespace = expired.namespace AND member.name = expired.name
			AND member.kind = expired.kind AND member.value = expired.value
	`, exhausted.args...); err != nil {
		return errors.Wrap(err, "failed to delete the exhausted openlineage dataset members")
	}
	return nil
}

// countOpenLineageDatasetJob moves a dataset's job count for the direction a task
// member belongs to. An integration or a source is a display value: the arrays are
// read from the member rows themselves, so they carry no count of their own.
func countOpenLineageDatasetJob(jobDeltas map[datasetKey]*openLineageDatasetJobDelta, dataset datasetKey, kind string, delta int32) {
	if kind != openLineageDatasetMemberInputTask && kind != openLineageDatasetMemberOutputTask {
		return
	}
	jobs := jobDeltas[dataset]
	if jobs == nil {
		jobs = &openLineageDatasetJobDelta{}
		jobDeltas[dataset] = jobs
	}
	if kind == openLineageDatasetMemberInputTask {
		jobs.source += delta
		return
	}
	jobs.target += delta
}

// updateOpenLineageDatasetAggregates writes each dataset's new counters and drops
// the ones whose last reference went away. A dataset whose newest reference was
// taken out has its newest event time read back, which only that row pays for.
func updateOpenLineageDatasetAggregates(ctx context.Context, tx *sql.Tx, deltas []*openLineageDatasetDelta, states map[datasetKey]*openLineageDatasetState, jobDeltas map[datasetKey]*openLineageDatasetJobDelta) error {
	emptied := newValuesList("text", "text")
	updated := newValuesList("text", "text", "bigint", "int", "int", "bigint", "boolean", "timestamptz")
	flush := func() error {
		if emptied.count() > 0 {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM openlineage_dataset ds
				USING (VALUES `+emptied.String()+`) AS gone(namespace, name)
				WHERE ds.namespace = gone.namespace AND ds.name = gone.name
			`, emptied.args...); err != nil {
				return errors.Wrap(err, "failed to delete the empty openlineage datasets")
			}
			emptied = newValuesList("text", "text")
		}
		if updated.count() > 0 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE openlineage_dataset ds SET
					ref_count = delta.ref_count,
					source_job_count = delta.source_job_count,
					target_job_count = delta.target_job_count,
					column_lineage_ref_count = delta.column_lineage_ref_count,
					last_seen = CASE
						WHEN delta.recompute THEN (
							SELECT MAX(remaining.event_time) FROM openlineage_run_dataset remaining
							WHERE remaining.namespace = ds.namespace AND remaining.name = ds.name
						)
						ELSE GREATEST(ds.last_seen, delta.added_last_seen)
					END,
					updated_at = NOW()
				FROM (VALUES `+updated.String()+`) AS delta(
					namespace, name, ref_count, source_job_count, target_job_count,
					column_lineage_ref_count, recompute, added_last_seen
				)
				WHERE ds.namespace = delta.namespace AND ds.name = delta.name
			`, updated.args...); err != nil {
				return errors.Wrap(err, "failed to update the openlineage dataset aggregates")
			}
			updated = newValuesList("text", "text", "bigint", "int", "int", "bigint", "boolean", "timestamptz")
		}
		return nil
	}

	for start := 0; start < len(deltas); start += maxOpenLineageDatasetAggregateRows {
		end := min(start+maxOpenLineageDatasetAggregateRows, len(deltas))
		for _, delta := range deltas[start:end] {
			key := datasetKey{namespace: delta.namespace, name: delta.name}
			state := states[key]
			if state == nil {
				return errors.Errorf("openlineage dataset %q has no aggregate row to update", delta.name)
			}
			if state.RefCount+int64(delta.refDelta) <= 0 {
				// The dataset's last reference went away. Its member rows are empty
				// by construction and cascade with the row.
				emptied.add(delta.namespace, delta.name)
				continue
			}
			sourceJobCount, targetJobCount := state.SourceJobCount, state.TargetJobCount
			if jobs := jobDeltas[key]; jobs != nil {
				sourceJobCount += jobs.source
				targetJobCount += jobs.target
			}
			var addedLastSeen any
			if delta.lastSeen != nil {
				addedLastSeen = *delta.lastSeen
			}
			updated.add(
				delta.namespace,
				delta.name,
				state.RefCount+int64(delta.refDelta),
				sourceJobCount,
				targetJobCount,
				state.ColumnLineageRefCount+int64(delta.columnLineageRefDelta),
				delta.removed,
				addedLastSeen,
			)
		}
		if err := flush(); err != nil {
			return err
		}
	}
	return flush()
}

func sortedOpenLineageDatasetMemberKeys(members map[openLineageDatasetMemberKey]int) []openLineageDatasetMemberKey {
	keys := make([]openLineageDatasetMemberKey, 0, len(members))
	for key := range members {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b openLineageDatasetMemberKey) int {
		if diff := strings.Compare(a.kind, b.kind); diff != 0 {
			return diff
		}
		return strings.Compare(a.value, b.value)
	})
	return keys
}
