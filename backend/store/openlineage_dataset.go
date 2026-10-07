package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
)

// The directions a persisted run can reference a dataset in.
const (
	OpenLineageDatasetDirectionInput  = "input"
	OpenLineageDatasetDirectionOutput = "output"
)

// maxOpenLineageDatasetGroups bounds the dataset window one request reads. The
// dataset list, its filter menus and the detail's GUID resolution all read the
// same window — the most recently seen datasets — so a dataset the list can show
// is always one the detail can resolve, and a workspace that accumulates more
// datasets than this does not make a page load read them all. It is larger than
// any page (1000) a caller can ask for.
const maxOpenLineageDatasetGroups = 5000

// maxOpenLineageDatasetRefRows bounds how many references one INSERT statement
// writes, so an event naming many datasets stays under the parameter limit.
const maxOpenLineageDatasetRefRows = 100

// maxOpenLineageDatasetDetailPairs bounds the spellings one detail request
// reads. A resolved dataset is usually reported under a single namespace/name
// pair; the cap only exists so a producer cannot make one request read an
// unbounded set of equivalent spellings.
const maxOpenLineageDatasetDetailPairs = 64

// maxOpenLineageDatasetRecentRefs bounds how many references the facet, job and
// run queries of a dataset detail read, newest first. The summary reads the
// maintained aggregate instead, but these three still expand the references
// themselves — the widest schema, the related jobs and the recent runs they
// report are all within the newest references, and without the bound the detail
// would expand (and detoast) the facets of every run that ever touched the
// dataset.
const maxOpenLineageDatasetRecentRefs = 100

// maxOpenLineageDatasetDistinctValues bounds the integrations and sources one
// dataset shows. They are display values, and a producer that varies them per
// event could otherwise grow one dataset's array without bound. The member table
// keeps every value, so a filter by one that is not shown still matches.
const maxOpenLineageDatasetDistinctValues = 16

// maxOpenLineageDatasetColumnFields bounds the distinct column-lineage field
// names a detail request returns.
const maxOpenLineageDatasetColumnFields = 10000

// openLineageDatasetWindowCTE renders the window every dataset read works from:
// the most recently seen datasets, newest first. The list filters within it, and
// the detail resolves the requested GUID against exactly it, so the two cannot
// disagree about which datasets exist. The CTE is materialized on purpose: a
// filtered list must not be able to reach past the cap, so the LIMIT has to be
// applied before the filters rather than be merged into the outer query.
func openLineageDatasetWindowCTE() string {
	return `WITH dataset_window AS MATERIALIZED (
		SELECT namespace, name, last_seen, source_job_count, target_job_count, column_lineage_ref_count
		FROM openlineage_dataset
		ORDER BY last_seen DESC NULLS LAST, name, namespace
		LIMIT ` + strconv.Itoa(maxOpenLineageDatasetGroups) + `
	)`
}

// openLineageDatasetMemberArrayColumn renders the integrations or sources of one
// dataset of the window. The member rows are ordered by value, so a redelivery
// cannot reorder what a page shows, and read through the primary key of the
// member table, so the ordered, capped read stops early instead of scanning the
// dataset's values.
func openLineageDatasetMemberArrayColumn(alias, kind string) string {
	return `COALESCE((SELECT array_agg(value ORDER BY value) FROM (
			SELECT value FROM openlineage_dataset_member
			WHERE namespace = ` + alias + `.namespace AND name = ` + alias + `.name AND kind = '` + kind + `'
			ORDER BY value LIMIT ` + strconv.Itoa(maxOpenLineageDatasetDistinctValues) + `
		) ordered_values), '{}')`
}

// openLineageDatasetMemberExistsClause renders the predicate a filter by an
// integration or source pushes into SQL. It is an equality probe on the member
// table's primary key, so filtering the window never reads a dataset's
// references.
func openLineageDatasetMemberExistsClause(alias, kind string, argIndex int) string {
	return fmt.Sprintf(`EXISTS (
			SELECT 1 FROM openlineage_dataset_member filter_member
			WHERE filter_member.namespace = %s.namespace AND filter_member.name = %s.name
				AND filter_member.kind = '%s' AND filter_member.value = $%d
		)`, alias, alias, kind, argIndex)
}

// openLineageDatasetRecentRefsCTE renders the bounded, newest-first subquery a
// detail's facet, job and run queries read.
func openLineageDatasetRecentRefsCTE(clause string) string {
	return `WITH recent AS (
		SELECT d.id, d.run_pk, d.task_guid, d.direction, d.event_time, d.schema_fields, d.column_lineage_fields
		FROM openlineage_run_dataset d
		WHERE ` + clause + `
		ORDER BY d.event_time DESC NULLS LAST, d.id DESC
		LIMIT ` + strconv.Itoa(maxOpenLineageDatasetRecentRefs) + `
	)`
}

// OpenLineageRunDatasetMessage is one dataset a persisted run read or wrote.
type OpenLineageRunDatasetMessage struct {
	TaskGUID         string
	Namespace        string
	Name             string
	Direction        string
	HasColumnLineage bool
	// SchemaFields and ColumnLineageFields hold the facet JSON itself, like
	// raw_payload on the run, and are nil when the payload carried no facet.
	SchemaFields        []byte
	ColumnLineageFields []byte
	EventTime           *time.Time
	Integration         string
	Source              string
}

// FindOpenLineageDatasetMessage filters the dataset window. The filters it
// carries are the ones the aggregate itself can answer: the free-text search and
// the internal/external scope need resolution and stay with the caller.
type FindOpenLineageDatasetMessage struct {
	Namespace         *string
	Integration       *string
	Source            *string
	ColumnLineageOnly *bool
}

// OpenLineageDatasetAggregateMessage is one dataset of the window, with the
// aggregate the list shows.
type OpenLineageDatasetAggregateMessage struct {
	Namespace             string
	Name                  string
	LastSeen              *time.Time
	SourceJobCount        int32
	TargetJobCount        int32
	Integrations          []string
	Sources               []string
	SupportsColumnLineage bool
}

// OpenLineageDatasetPair is one (namespace, name) spelling of a dataset.
type OpenLineageDatasetPair struct {
	Namespace string
	Name      string
}

// OpenLineageDatasetJobMessage is one job that touched a dataset.
type OpenLineageDatasetJobMessage struct {
	TaskGUID      string
	JobNamespace  string
	JobName       string
	JobType       string
	Integration   string
	LastSeen      *time.Time
	RunCount      int32
	ReadsDataset  bool
	WritesDataset bool
}

// OpenLineageDatasetRunMessage is one run that touched a dataset.
type OpenLineageDatasetRunMessage struct {
	RunGUID       string
	TaskGUID      string
	RunID         string
	JobNamespace  string
	JobName       string
	JobType       string
	EventType     string
	EventTime     *time.Time
	HasLineage    bool
	ReadsDataset  bool
	WritesDataset bool
}

// OpenLineageDatasetDetailMessage is a dataset's detail. The summary comes from
// the maintained aggregate — a single spelling reads one row — while the schema,
// jobs and runs are read from a bounded, newest-first window of references. The
// dataset's own identity and resolution stay with the caller.
type OpenLineageDatasetDetailMessage struct {
	LastSeen              *time.Time
	SourceJobCount        int32
	TargetJobCount        int32
	Integrations          []string
	Sources               []string
	SupportsColumnLineage bool
	Jobs                  []*OpenLineageDatasetJobMessage
	Runs                  []*OpenLineageDatasetRunMessage
	SchemaFields          []byte
	ColumnLineageFields   []string
}

// replaceOpenLineageRunDatasets rewrites one run's dataset references. The run
// row is the run's latest known state, so its references are replaced as a set
// rather than merged: a redelivery that drops a dataset has to drop it here too.
// The per-dataset aggregate is folded in once the transaction's references are
// written — see applyOpenLineageDatasetDeltas.
func replaceOpenLineageRunDatasets(ctx context.Context, tx *sql.Tx, runPK int64, refs []*OpenLineageRunDatasetMessage) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM openlineage_run_dataset WHERE run_pk = $1`, runPK); err != nil {
		return errors.Wrap(err, "failed to clear the openlineage run's datasets")
	}
	if len(refs) == 0 {
		return nil
	}

	for start := 0; start < len(refs); start += maxOpenLineageDatasetRefRows {
		end := min(start+maxOpenLineageDatasetRefRows, len(refs))
		values := make([]string, 0, end-start)
		args := make([]any, 0, (end-start)*11)
		for _, ref := range refs[start:end] {
			base := len(args)
			values = append(values, fmt.Sprintf(
				"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11,
			))
			var eventTime any
			if ref.EventTime != nil {
				eventTime = *ref.EventTime
			}
			args = append(args,
				runPK,
				ref.TaskGUID,
				ref.Namespace,
				ref.Name,
				ref.Direction,
				ref.HasColumnLineage,
				emptyJSONBToNil(ref.SchemaFields),
				emptyJSONBToNil(ref.ColumnLineageFields),
				eventTime,
				ref.Integration,
				ref.Source,
			)
		}
		query := `INSERT INTO openlineage_run_dataset (
			run_pk, task_guid, namespace, name, direction, has_column_lineage,
			schema_fields, column_lineage_fields, event_time, integration, source
		) VALUES ` + strings.Join(values, ", ")
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return errors.Wrap(err, "failed to insert the openlineage run's datasets")
		}
	}
	return nil
}

// listOpenLineageRunDatasets reads the references a stored run holds, so a
// replacement can hand the aggregate the set it is about to take out.
func listOpenLineageRunDatasets(ctx context.Context, tx *sql.Tx, runPK int64) ([]*OpenLineageRunDatasetMessage, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT task_guid, namespace, name, direction, has_column_lineage, event_time, integration, source
		FROM openlineage_run_dataset
		WHERE run_pk = $1
	`, runPK)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the openlineage run's datasets")
	}
	defer rows.Close()

	var refs []*OpenLineageRunDatasetMessage
	for rows.Next() {
		var ref OpenLineageRunDatasetMessage
		var eventTime sql.NullTime
		if err := rows.Scan(
			&ref.TaskGUID,
			&ref.Namespace,
			&ref.Name,
			&ref.Direction,
			&ref.HasColumnLineage,
			&eventTime,
			&ref.Integration,
			&ref.Source,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan an openlineage run dataset")
		}
		if eventTime.Valid {
			t := eventTime.Time
			ref.EventTime = &t
		}
		refs = append(refs, &ref)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate the openlineage run's datasets")
	}
	return refs, nil
}

// emptyJSONBToNil turns an absent facet into SQL NULL. A zero-length []byte
// would otherwise reach the JSONB column as an empty value and fail to parse.
func emptyJSONBToNil(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// ListOpenLineageDatasetAggregate reads the dataset window with the aggregate
// the list shows. The filters it answers in SQL are applied inside the window,
// never to widen it: the detail resolves the requested GUID against the
// unfiltered window, so a filtered list must not offer a dataset the detail
// cannot open.
func (s *Store) ListOpenLineageDatasetAggregate(ctx context.Context, find *FindOpenLineageDatasetMessage) ([]*OpenLineageDatasetAggregateMessage, error) {
	query, args := buildOpenLineageDatasetAggregateQuery(find)
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query openlineage dataset aggregates")
	}
	return scanOpenLineageDatasetAggregateRows(rows)
}

// buildOpenLineageDatasetAggregateQuery renders the dataset list. The window is
// bounded first and the filters are applied to it afterwards, so the list shows
// at most the datasets the detail can resolve.
func buildOpenLineageDatasetAggregateQuery(find *FindOpenLineageDatasetMessage) (string, []any) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.Namespace; v != nil {
		args = append(args, *v)
		where = append(where, fmt.Sprintf("w.namespace = $%d", len(args)))
	}
	if v := find.Integration; v != nil {
		args = append(args, *v)
		where = append(where, openLineageDatasetMemberExistsClause("w", openLineageDatasetMemberIntegration, len(args)))
	}
	if v := find.Source; v != nil {
		args = append(args, *v)
		where = append(where, openLineageDatasetMemberExistsClause("w", openLineageDatasetMemberSource, len(args)))
	}
	if find.ColumnLineageOnly != nil && *find.ColumnLineageOnly {
		where = append(where, "w.column_lineage_ref_count > 0")
	}

	query := openLineageDatasetWindowCTE() + `
		SELECT
			w.namespace,
			w.name,
			w.last_seen,
			w.source_job_count,
			w.target_job_count,
			` + openLineageDatasetMemberArrayColumn("w", openLineageDatasetMemberIntegration) + `,
			` + openLineageDatasetMemberArrayColumn("w", openLineageDatasetMemberSource) + `,
			w.column_lineage_ref_count > 0
		FROM dataset_window w
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY w.last_seen DESC NULLS LAST, w.name, w.namespace`
	return query, args
}

// ListOpenLineageDatasetWindow returns the spellings of the dataset window. The
// detail resolves the requested GUID against it, so the dataset a list row shows
// is always one the detail can open.
func (s *Store) ListOpenLineageDatasetWindow(ctx context.Context) ([]OpenLineageDatasetPair, error) {
	rows, err := s.GetDB().QueryContext(ctx, openLineageDatasetWindowCTE()+`
		SELECT w.namespace, w.name
		FROM dataset_window w
		ORDER BY w.last_seen DESC NULLS LAST, w.name, w.namespace`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the openlineage dataset window")
	}
	defer rows.Close()

	var pairs []OpenLineageDatasetPair
	for rows.Next() {
		var pair OpenLineageDatasetPair
		if err := rows.Scan(&pair.Namespace, &pair.Name); err != nil {
			return nil, errors.Wrap(err, "failed to scan the openlineage dataset window")
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read the openlineage dataset window")
	}
	return pairs, nil
}

func scanOpenLineageDatasetAggregateRows(rows *sql.Rows) ([]*OpenLineageDatasetAggregateMessage, error) {
	defer rows.Close()

	var result []*OpenLineageDatasetAggregateMessage
	for rows.Next() {
		var msg OpenLineageDatasetAggregateMessage
		var lastSeen sql.NullTime
		var integrations, sources pq.StringArray
		if err := rows.Scan(
			&msg.Namespace,
			&msg.Name,
			&lastSeen,
			&msg.SourceJobCount,
			&msg.TargetJobCount,
			&integrations,
			&sources,
			&msg.SupportsColumnLineage,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan an openlineage dataset aggregate")
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			msg.LastSeen = &t
		}
		msg.Integrations = []string(integrations)
		msg.Sources = []string(sources)
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read openlineage dataset aggregates")
	}
	return result, nil
}

// GetOpenLineageDatasetDetail reads the detail of a dataset from the aggregate
// the runs maintain and from the references they left. The summary counts jobs
// over the union of the spellings, so a job that reported the dataset twice is
// still one job; with a single spelling it is one row of the aggregate, not an
// aggregation over the dataset's whole history.
func (s *Store) GetOpenLineageDatasetDetail(ctx context.Context, pairs []OpenLineageDatasetPair) (*OpenLineageDatasetDetailMessage, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	if len(pairs) > maxOpenLineageDatasetDetailPairs {
		pairs = pairs[:maxOpenLineageDatasetDetailPairs]
	}

	detail, matched, err := s.getOpenLineageDatasetSummary(ctx, pairs)
	if err != nil {
		return nil, err
	}
	if !matched {
		// No aggregate matches; the caller decides what a missing dataset means.
		return nil, nil
	}

	clause, args := openLineageDatasetPairClause(pairs, 0, "d")
	if detail.SchemaFields, err = s.getOpenLineageDatasetBestSchema(ctx, clause, args); err != nil {
		return nil, err
	}
	if detail.ColumnLineageFields, err = s.listOpenLineageDatasetColumnLineageFields(ctx, clause, args); err != nil {
		return nil, err
	}
	if detail.Jobs, err = s.listOpenLineageDatasetJobs(ctx, clause, args); err != nil {
		return nil, err
	}
	if detail.Runs, err = s.listOpenLineageDatasetRuns(ctx, clause, args); err != nil {
		return nil, err
	}
	return detail, nil
}

// getOpenLineageDatasetSummary reads the summary of the dataset the pairs name.
// One spelling answers from its own aggregate row; several are deduplicated
// through the member rows, because the same job reporting the dataset under two
// spellings is still one job.
func (s *Store) getOpenLineageDatasetSummary(ctx context.Context, pairs []OpenLineageDatasetPair) (*OpenLineageDatasetDetailMessage, bool, error) {
	clause, args := openLineageDatasetPairClause(pairs, 0, "ds")
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT
			ds.last_seen,
			ds.source_job_count,
			ds.target_job_count,
			ds.column_lineage_ref_count > 0,
			`+openLineageDatasetMemberArrayColumn("ds", openLineageDatasetMemberIntegration)+`,
			`+openLineageDatasetMemberArrayColumn("ds", openLineageDatasetMemberSource)+`
		FROM openlineage_dataset ds
		WHERE `+clause,
		args...,
	)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to summarize an openlineage dataset")
	}
	defer rows.Close()

	detail := &OpenLineageDatasetDetailMessage{}
	integrations := map[string]struct{}{}
	sources := map[string]struct{}{}
	spellings := 0
	for rows.Next() {
		var lastSeen sql.NullTime
		var sourceJobCount, targetJobCount int32
		var supportsColumnLineage bool
		var rowIntegrations, rowSources pq.StringArray
		if err := rows.Scan(
			&lastSeen,
			&sourceJobCount,
			&targetJobCount,
			&supportsColumnLineage,
			&rowIntegrations,
			&rowSources,
		); err != nil {
			return nil, false, errors.Wrap(err, "failed to scan an openlineage dataset summary")
		}
		spellings++
		if lastSeen.Valid && (detail.LastSeen == nil || lastSeen.Time.After(*detail.LastSeen)) {
			t := lastSeen.Time
			detail.LastSeen = &t
		}
		detail.SourceJobCount += sourceJobCount
		detail.TargetJobCount += targetJobCount
		detail.SupportsColumnLineage = detail.SupportsColumnLineage || supportsColumnLineage
		for _, value := range rowIntegrations {
			integrations[value] = struct{}{}
		}
		for _, value := range rowSources {
			sources[value] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, errors.Wrap(err, "failed to read an openlineage dataset summary")
	}
	if spellings == 0 {
		return nil, false, nil
	}

	detail.Integrations = sortedCappedKeys(integrations)
	detail.Sources = sortedCappedKeys(sources)
	if spellings > 1 {
		// Summing the spellings would count a job that reported the dataset
		// twice twice, so the union is deduplicated over the task members of the
		// pairs. It reads one row per distinct (job, direction) of those
		// spellings, not one per run.
		source, target, err := s.countOpenLineageDatasetSpellingJobs(ctx, pairs)
		if err != nil {
			return nil, false, err
		}
		detail.SourceJobCount = source
		detail.TargetJobCount = target
	}
	return detail, true, nil
}

// countOpenLineageDatasetSpellingJobs counts the distinct jobs that reference
// any of the given spellings, per direction.
func (s *Store) countOpenLineageDatasetSpellingJobs(ctx context.Context, pairs []OpenLineageDatasetPair) (source, target int32, err error) {
	clause, args := openLineageDatasetPairClause(pairs, 0, "m")
	if err := s.GetDB().QueryRowContext(ctx, `
		SELECT
			COUNT(DISTINCT value) FILTER (WHERE kind = '`+openLineageDatasetMemberInputTask+`'),
			COUNT(DISTINCT value) FILTER (WHERE kind = '`+openLineageDatasetMemberOutputTask+`')
		FROM openlineage_dataset_member m
		WHERE `+clause,
		args...,
	).Scan(&source, &target); err != nil {
		return 0, 0, errors.Wrap(err, "failed to count the openlineage dataset's jobs")
	}
	return source, target, nil
}

// sortedCappedKeys orders a value set for a page and drops its tail.
func sortedCappedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	slices.Sort(keys)
	if len(keys) > maxOpenLineageDatasetDistinctValues {
		keys = keys[:maxOpenLineageDatasetDistinctValues]
	}
	return keys
}

// getOpenLineageDatasetBestSchema returns the widest schema facet among the
// dataset's most recent references, breaking a tie by recency: a dataset's
// schema is best described by the run that saw the most of it, and among equals
// by the latest one. Only the newest references are considered, because the
// alternative — sorting every facet the dataset ever had — is what made one
// detail request read an unbounded amount of JSONB.
func (s *Store) getOpenLineageDatasetBestSchema(ctx context.Context, clause string, args []any) ([]byte, error) {
	var schemaFields []byte
	err := s.GetDB().QueryRowContext(ctx, openLineageDatasetRecentRefsCTE(clause)+`
		SELECT recent.schema_fields
		FROM recent
		WHERE recent.schema_fields IS NOT NULL
		ORDER BY jsonb_array_length(recent.schema_fields) DESC, recent.event_time DESC NULLS LAST
		LIMIT 1`,
		args...,
	).Scan(&schemaFields)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read the openlineage dataset schema")
	}
	return schemaFields, nil
}

func (s *Store) listOpenLineageDatasetColumnLineageFields(ctx context.Context, clause string, args []any) ([]string, error) {
	queryArgs := append(append([]any{}, args...), maxOpenLineageDatasetColumnFields)
	rows, err := s.GetDB().QueryContext(ctx, openLineageDatasetRecentRefsCTE(clause)+`
		SELECT DISTINCT jsonb_array_elements_text(recent.column_lineage_fields) AS field
		FROM recent
		WHERE recent.direction = '`+OpenLineageDatasetDirectionOutput+`' AND recent.column_lineage_fields IS NOT NULL
		ORDER BY field
		LIMIT $`+strconv.Itoa(len(queryArgs)),
		queryArgs...,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the openlineage dataset column lineage fields")
	}
	defer rows.Close()

	var fields []string
	for rows.Next() {
		var field string
		if err := rows.Scan(&field); err != nil {
			return nil, errors.Wrap(err, "failed to scan an openlineage dataset column lineage field")
		}
		fields = append(fields, field)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read openlineage dataset column lineage fields")
	}
	return fields, nil
}

// listOpenLineageDatasetJobs returns the jobs that touched the dataset, most
// recently seen first. The integration is the newest run's, which is what the
// per-run aggregation used to report. The newest references carry every job in
// the answer — a job in the top eight by last-seen has one of them — so the
// bounded subquery cannot drop one.
func (s *Store) listOpenLineageDatasetJobs(ctx context.Context, clause string, args []any) ([]*OpenLineageDatasetJobMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, openLineageDatasetRecentRefsCTE(clause)+`
		SELECT
			recent.task_guid,
			r.job_namespace,
			r.job_name,
			r.job_type,
			(ARRAY_AGG(r.integration ORDER BY r.event_time DESC NULLS LAST, r.id DESC))[1],
			MAX(r.event_time),
			COUNT(DISTINCT r.id),
			BOOL_OR(recent.direction = '`+OpenLineageDatasetDirectionInput+`'),
			BOOL_OR(recent.direction = '`+OpenLineageDatasetDirectionOutput+`')
		FROM recent
		JOIN openlineage_run r ON r.id = recent.run_pk
		GROUP BY recent.task_guid, r.job_namespace, r.job_name, r.job_type
		ORDER BY MAX(r.event_time) DESC NULLS LAST, r.job_name, recent.task_guid
		LIMIT 8`,
		args...,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the openlineage dataset's jobs")
	}
	defer rows.Close()

	var result []*OpenLineageDatasetJobMessage
	for rows.Next() {
		var msg OpenLineageDatasetJobMessage
		var lastSeen sql.NullTime
		if err := rows.Scan(
			&msg.TaskGUID,
			&msg.JobNamespace,
			&msg.JobName,
			&msg.JobType,
			&msg.Integration,
			&lastSeen,
			&msg.RunCount,
			&msg.ReadsDataset,
			&msg.WritesDataset,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan an openlineage dataset job")
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			msg.LastSeen = &t
		}
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read the openlineage dataset's jobs")
	}
	return result, nil
}

// listOpenLineageDatasetRuns returns the runs that touched the dataset, newest
// first. Every run in the answer has one of the newest references, so the
// bounded subquery cannot drop one.
func (s *Store) listOpenLineageDatasetRuns(ctx context.Context, clause string, args []any) ([]*OpenLineageDatasetRunMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, openLineageDatasetRecentRefsCTE(clause)+`
		SELECT
			r.guid,
			r.task_guid,
			r.run_id,
			r.job_namespace,
			r.job_name,
			r.job_type,
			r.event_type,
			r.event_time,
			r.has_lineage,
			BOOL_OR(recent.direction = '`+OpenLineageDatasetDirectionInput+`'),
			BOOL_OR(recent.direction = '`+OpenLineageDatasetDirectionOutput+`')
		FROM recent
		JOIN openlineage_run r ON r.id = recent.run_pk
		GROUP BY r.id, r.guid, r.task_guid, r.run_id, r.job_namespace, r.job_name, r.job_type, r.event_type, r.event_time, r.has_lineage
		ORDER BY r.event_time DESC NULLS LAST, r.run_id, r.id DESC
		LIMIT 10`,
		args...,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query the openlineage dataset's runs")
	}
	defer rows.Close()

	var result []*OpenLineageDatasetRunMessage
	for rows.Next() {
		var msg OpenLineageDatasetRunMessage
		var eventTime sql.NullTime
		if err := rows.Scan(
			&msg.RunGUID,
			&msg.TaskGUID,
			&msg.RunID,
			&msg.JobNamespace,
			&msg.JobName,
			&msg.JobType,
			&msg.EventType,
			&eventTime,
			&msg.HasLineage,
			&msg.ReadsDataset,
			&msg.WritesDataset,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan an openlineage dataset run")
		}
		if eventTime.Valid {
			t := eventTime.Time
			msg.EventTime = &t
		}
		result = append(result, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read the openlineage dataset's runs")
	}
	return result, nil
}

// ListOpenLineageDatasetFilterValues returns the dataset dimensions the dataset
// page's filter menus offer, most common first, read from the same window the
// list reads: a menu value that no dataset of its window carries would filter
// the list down to nothing. Each count is the number of datasets carrying the
// value.
func (s *Store) ListOpenLineageDatasetFilterValues(ctx context.Context) ([]*OpenLineageFilterValue, error) {
	rows, err := s.GetDB().QueryContext(ctx, openLineageDatasetWindowCTE()+`
		SELECT '`+openLineageFilterDatasetNamespace+`', w.namespace, COUNT(*) FROM dataset_window w WHERE w.namespace <> '' GROUP BY 2
		UNION ALL
		SELECT '`+openLineageFilterDatasetIntegration+`', m.value, COUNT(*) FROM dataset_window w
			JOIN openlineage_dataset_member m ON m.namespace = w.namespace AND m.name = w.name
			WHERE m.kind = '`+openLineageDatasetMemberIntegration+`' AND m.value <> '' GROUP BY 2
		UNION ALL
		SELECT '`+openLineageFilterDatasetSource+`', m.value, COUNT(*) FROM dataset_window w
			JOIN openlineage_dataset_member m ON m.namespace = w.namespace AND m.name = w.name
			WHERE m.kind = '`+openLineageDatasetMemberSource+`' AND m.value <> '' GROUP BY 2
		ORDER BY 1, 3 DESC, 2`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list openlineage dataset filter values")
	}
	return collectOpenLineageFilterValues(rows)
}

// openLineageDatasetPairClause renders the predicate selecting exactly the
// given (namespace, name) spellings under one table alias. The pairs are bound
// positionally so a namespace of one pair cannot combine with the name of
// another.
func openLineageDatasetPairClause(pairs []OpenLineageDatasetPair, startIndex int, alias string) (string, []any) {
	clauses := make([]string, 0, len(pairs))
	args := make([]any, 0, len(pairs)*2)
	for _, pair := range pairs {
		clauses = append(clauses, fmt.Sprintf(
			"(%s.namespace = $%d AND %s.name = $%d)",
			alias, startIndex+len(args)+1, alias, startIndex+len(args)+2,
		))
		args = append(args, pair.Namespace, pair.Name)
	}
	return strings.Join(clauses, " OR "), args
}
