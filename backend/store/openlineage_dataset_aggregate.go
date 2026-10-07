package store

import (
	"context"
	"database/sql"
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
		if delta.refDelta == 0 && delta.columnLineageRefDelta == 0 && len(delta.members) == 0 {
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

// applyOpenLineageDatasetDeltas folds one ingest transaction's run replacements
// into the per-dataset aggregate. It runs after the references are written and
// after every task lock of the transaction has been taken: the dataset rows and
// the member rows under them are then locked in (namespace, name) order, which
// keeps a batch's lock order global — tasks ascending, then datasets ascending —
// so two batches cannot deadlock on each other's datasets.
func applyOpenLineageDatasetDeltas(ctx context.Context, tx *sql.Tx, replacements []openLineageRunDatasetReplacement) error {
	for _, delta := range openLineageDatasetDeltas(replacements) {
		if err := applyOpenLineageDatasetDelta(ctx, tx, delta); err != nil {
			return err
		}
	}
	return nil
}

// applyOpenLineageDatasetDelta folds one dataset's delta into its aggregate row.
func applyOpenLineageDatasetDelta(ctx context.Context, tx *sql.Tx, delta *openLineageDatasetDelta) error {
	state, err := lockOpenLineageDataset(ctx, tx, delta.namespace, delta.name)
	if err != nil {
		return err
	}

	var sourceJobDelta, targetJobDelta int32
	for _, member := range sortedOpenLineageDatasetMemberKeys(delta.members) {
		created, exhausted, err := applyOpenLineageDatasetMemberDelta(ctx, tx, delta.namespace, delta.name, member, delta.members[member])
		if err != nil {
			return err
		}
		switch member.kind {
		case openLineageDatasetMemberInputTask:
			if created {
				sourceJobDelta++
			}
			if exhausted {
				sourceJobDelta--
			}
		case openLineageDatasetMemberOutputTask:
			if created {
				targetJobDelta++
			}
			if exhausted {
				targetJobDelta--
			}
		case openLineageDatasetMemberIntegration, openLineageDatasetMemberSource:
			// Display values carry no count of their own: the arrays are read
			// from the member rows themselves.
		default:
		}
	}

	refCount := state.RefCount + int64(delta.refDelta)
	if refCount <= 0 {
		// The dataset's last reference went away. Its member rows are empty by
		// construction and cascade with the row.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM openlineage_dataset WHERE namespace = $1 AND name = $2`,
			delta.namespace, delta.name,
		); err != nil {
			return errors.Wrap(err, "failed to delete the empty openlineage dataset")
		}
		return nil
	}

	lastSeen := state.LastSeen
	if delta.removed {
		// A reference that was part of the dataset is gone, and it may have held
		// the newest event time. The (namespace, name, event_time) index answers
		// the read without touching the references' other columns.
		recomputed, err := latestOpenLineageDatasetRefEventTime(ctx, tx, delta.namespace, delta.name)
		if err != nil {
			return err
		}
		lastSeen = recomputed
	} else if delta.lastSeen != nil && (lastSeen == nil || delta.lastSeen.After(*lastSeen)) {
		lastSeen = delta.lastSeen
	}

	var lastSeenValue any
	if lastSeen != nil {
		lastSeenValue = *lastSeen
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE openlineage_dataset SET
			ref_count = $1,
			source_job_count = $2,
			target_job_count = $3,
			column_lineage_ref_count = $4,
			last_seen = $5,
			updated_at = NOW()
		WHERE namespace = $6 AND name = $7
	`,
		refCount,
		state.SourceJobCount+sourceJobDelta,
		state.TargetJobCount+targetJobDelta,
		state.ColumnLineageRefCount+int64(delta.columnLineageRefDelta),
		lastSeenValue,
		delta.namespace,
		delta.name,
	); err != nil {
		return errors.Wrap(err, "failed to update the openlineage dataset aggregate")
	}
	return nil
}

// openLineageDatasetState is a dataset's stored aggregate, read while its row is
// locked for one ingest transaction.
type openLineageDatasetState struct {
	RefCount              int64
	SourceJobCount        int32
	TargetJobCount        int32
	ColumnLineageRefCount int64
	LastSeen              *time.Time
}

// lockOpenLineageDataset takes the dataset's row lock and returns its stored
// aggregate. The insert makes the row exist for the member rows that follow, and
// the no-op update is what locks a row that is already there; the lock is what
// makes the counters exact, since two writers of one dataset then serialize.
func lockOpenLineageDataset(ctx context.Context, tx *sql.Tx, namespace, name string) (*openLineageDatasetState, error) {
	var state openLineageDatasetState
	var lastSeen sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO openlineage_dataset (namespace, name)
		VALUES ($1, $2)
		ON CONFLICT (namespace, name) DO UPDATE SET updated_at = openlineage_dataset.updated_at
		RETURNING ref_count, source_job_count, target_job_count, column_lineage_ref_count, last_seen
	`, namespace, name).Scan(
		&state.RefCount,
		&state.SourceJobCount,
		&state.TargetJobCount,
		&state.ColumnLineageRefCount,
		&lastSeen,
	); err != nil {
		return nil, errors.Wrap(err, "failed to lock openlineage dataset")
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		state.LastSeen = &t
	}
	return &state, nil
}

// applyOpenLineageDatasetMemberDelta moves one member's reference count and
// reports whether the member row was created (the dataset gained a job or a
// value) or exhausted (it lost its last one). Rows never hold a zero count, so a
// positive result equal to the delta is an insert, and a zero result is the last
// reference.
func applyOpenLineageDatasetMemberDelta(ctx context.Context, tx *sql.Tx, namespace, name string, key openLineageDatasetMemberKey, delta int) (created, exhausted bool, err error) {
	if delta > 0 {
		var refCount int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO openlineage_dataset_member (namespace, name, kind, value, ref_count)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (namespace, name, kind, value)
			DO UPDATE SET ref_count = openlineage_dataset_member.ref_count + EXCLUDED.ref_count
			RETURNING ref_count
		`, namespace, name, key.kind, key.value, delta).Scan(&refCount); err != nil {
			return false, false, errors.Wrap(err, "failed to count an openlineage dataset member")
		}
		return refCount == int64(delta), false, nil
	}

	var refCount int64
	if err := tx.QueryRowContext(ctx, `
		UPDATE openlineage_dataset_member SET ref_count = ref_count + $5
		WHERE namespace = $1 AND name = $2 AND kind = $3 AND value = $4
		RETURNING ref_count
	`, namespace, name, key.kind, key.value, delta).Scan(&refCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The member is not there to take anything out of; the dataset row
			// decides what stands.
			return false, false, nil
		}
		return false, false, errors.Wrap(err, "failed to count an openlineage dataset member")
	}
	if refCount > 0 {
		return false, false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM openlineage_dataset_member
		WHERE namespace = $1 AND name = $2 AND kind = $3 AND value = $4
	`, namespace, name, key.kind, key.value); err != nil {
		return false, false, errors.Wrap(err, "failed to delete an exhausted openlineage dataset member")
	}
	return false, true, nil
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

// latestOpenLineageDatasetRefEventTime reads the newest event time of the
// dataset's remaining references.
func latestOpenLineageDatasetRefEventTime(ctx context.Context, tx *sql.Tx, namespace, name string) (*time.Time, error) {
	var latest sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT MAX(event_time) FROM openlineage_run_dataset WHERE namespace = $1 AND name = $2
	`, namespace, name).Scan(&latest); err != nil {
		return nil, errors.Wrap(err, "failed to read the newest openlineage dataset reference")
	}
	if !latest.Valid {
		return nil, nil
	}
	t := latest.Time
	return &t, nil
}
