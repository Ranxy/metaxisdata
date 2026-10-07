package store

import (
	"context"
	"database/sql"
	"fmt"
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

// maxOpenLineageDatasetGroups bounds the dataset aggregate one request reads.
// The dataset list and detail resolve, filter and page that aggregate in
// memory, and a workspace can accumulate an unbounded number of distinct
// datasets, so the read has to be capped. The cap keeps the most recently seen
// datasets — the same window the payload scan this replaces kept — and is
// larger than any page (1000) a caller can ask for.
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
// run queries of a dataset detail read, newest first. The whole-history summary
// cannot be bounded this way, but these three can: the widest schema, the
// related jobs and the recent runs they report are all within the newest
// references, and without the bound the detail would expand (and detoast) the
// facets of every run that ever touched the dataset.
const maxOpenLineageDatasetRecentRefs = 100

// maxOpenLineageDatasetDistinctValues bounds the integrations and sources one
// dataset group carries. They are display values, and a producer that varies
// them per event could otherwise grow one group's array without bound.
const maxOpenLineageDatasetDistinctValues = 16

// maxOpenLineageDatasetColumnFields bounds the distinct column-lineage field
// names a detail request returns.
const maxOpenLineageDatasetColumnFields = 10000

// openLineageDatasetAggregateColumns is the aggregate projection shared by the
// dataset list and the detail summary. Every count is over distinct task GUIDs,
// which is the "job" the dataset pages talk about. The value arrays are ordered
// so a redelivery cannot reorder what the page shows, and sliced so one group
// cannot grow them without bound.
func openLineageDatasetAggregateColumns() string {
	return `
		d.namespace,
		d.name,
		MAX(d.event_time) AS last_seen,
		COUNT(DISTINCT d.task_guid) FILTER (WHERE d.direction = '` + OpenLineageDatasetDirectionInput + `'),
		COUNT(DISTINCT d.task_guid) FILTER (WHERE d.direction = '` + OpenLineageDatasetDirectionOutput + `'),
		(COALESCE(ARRAY_AGG(DISTINCT d.integration ORDER BY d.integration) FILTER (WHERE d.integration <> ''), '{}'))[1:` + strconv.Itoa(maxOpenLineageDatasetDistinctValues) + `],
		(COALESCE(ARRAY_AGG(DISTINCT d.source ORDER BY d.source) FILTER (WHERE d.source <> ''), '{}'))[1:` + strconv.Itoa(maxOpenLineageDatasetDistinctValues) + `],
		BOOL_OR(d.has_column_lineage)`
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

// FindOpenLineageDatasetMessage filters the dataset aggregate. The filters it
// carries are the ones the aggregate itself can answer: the free-text search
// and the internal/external scope need resolution and stay with the caller.
type FindOpenLineageDatasetMessage struct {
	Namespace         *string
	Integration       *string
	Source            *string
	ColumnLineageOnly *bool
}

// OpenLineageDatasetAggregateMessage is one dataset aggregated over the runs
// that referenced it.
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

// OpenLineageDatasetDetailMessage is a dataset's detail, read from the
// references of every run that touched it. The dataset's own identity and
// resolution stay with the caller, which is what resolved the GUID.
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

// emptyJSONBToNil turns an absent facet into SQL NULL. A zero-length []byte
// would otherwise reach the JSONB column as an empty value and fail to parse.
func emptyJSONBToNil(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// ListOpenLineageDatasetAggregate aggregates the datasets the persisted runs
// referenced, most recently seen first, capped at maxOpenLineageDatasetGroups.
func (s *Store) ListOpenLineageDatasetAggregate(ctx context.Context, find *FindOpenLineageDatasetMessage) ([]*OpenLineageDatasetAggregateMessage, error) {
	query, args := buildOpenLineageDatasetAggregateQuery(find)
	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query openlineage dataset aggregates")
	}
	return scanOpenLineageDatasetAggregateRows(rows)
}

// buildOpenLineageDatasetAggregateQuery renders the dataset aggregate. The
// projection is grouped and capped so one request reads a bounded number of
// small rows instead of every run's payload. Integration, source and column
// lineage are properties of the group, so they belong in HAVING: filtering rows
// in WHERE would drop the other integrations of a group the filter kept, and
// the list shows them.
func buildOpenLineageDatasetAggregateQuery(find *FindOpenLineageDatasetMessage) (string, []any) {
	where, having, args := []string{"TRUE"}, []string{"TRUE"}, []any{}
	if v := find.Namespace; v != nil {
		args = append(args, *v)
		where = append(where, fmt.Sprintf("d.namespace = $%d", len(args)))
	}
	if v := find.Integration; v != nil {
		args = append(args, *v)
		having = append(having, fmt.Sprintf("BOOL_OR(d.integration = $%d)", len(args)))
	}
	if v := find.Source; v != nil {
		args = append(args, *v)
		having = append(having, fmt.Sprintf("BOOL_OR(d.source = $%d)", len(args)))
	}
	if find.ColumnLineageOnly != nil && *find.ColumnLineageOnly {
		having = append(having, "BOOL_OR(d.has_column_lineage)")
	}
	args = append(args, maxOpenLineageDatasetGroups)

	query := `SELECT ` + openLineageDatasetAggregateColumns() + `
		FROM openlineage_run_dataset d
		WHERE ` + strings.Join(where, " AND ") + `
		GROUP BY d.namespace, d.name
		HAVING ` + strings.Join(having, " AND ") + `
		ORDER BY last_seen DESC NULLS LAST, d.name, d.namespace
		LIMIT $` + strconv.Itoa(len(args))
	return query, args
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

// GetOpenLineageDatasetDetail reads the detail of a dataset from the runs that
// referenced any of its spellings. The summary counts jobs over the union of
// the spellings, so a job that reported the dataset twice is still one job.
func (s *Store) GetOpenLineageDatasetDetail(ctx context.Context, pairs []OpenLineageDatasetPair) (*OpenLineageDatasetDetailMessage, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	if len(pairs) > maxOpenLineageDatasetDetailPairs {
		pairs = pairs[:maxOpenLineageDatasetDetailPairs]
	}
	clause, args := openLineageDatasetPairClause(pairs, 0)

	detail, matched, err := s.getOpenLineageDatasetSummary(ctx, clause, args)
	if err != nil {
		return nil, err
	}
	if !matched {
		// No reference matches; the caller decides what a missing dataset means.
		return nil, nil
	}

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

func (s *Store) getOpenLineageDatasetSummary(ctx context.Context, clause string, args []any) (*OpenLineageDatasetDetailMessage, bool, error) {
	detail := &OpenLineageDatasetDetailMessage{}
	var matched int64
	var lastSeen sql.NullTime
	var integrations, sources pq.StringArray
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			MAX(d.event_time),
			COUNT(DISTINCT d.task_guid) FILTER (WHERE d.direction = '`+OpenLineageDatasetDirectionInput+`'),
			COUNT(DISTINCT d.task_guid) FILTER (WHERE d.direction = '`+OpenLineageDatasetDirectionOutput+`'),
			(COALESCE(ARRAY_AGG(DISTINCT d.integration ORDER BY d.integration) FILTER (WHERE d.integration <> ''), '{}'))[1:`+strconv.Itoa(maxOpenLineageDatasetDistinctValues)+`],
			(COALESCE(ARRAY_AGG(DISTINCT d.source ORDER BY d.source) FILTER (WHERE d.source <> ''), '{}'))[1:`+strconv.Itoa(maxOpenLineageDatasetDistinctValues)+`],
			BOOL_OR(d.has_column_lineage)
		FROM openlineage_run_dataset d
		WHERE `+clause,
		args...,
	).Scan(
		&matched,
		&lastSeen,
		&detail.SourceJobCount,
		&detail.TargetJobCount,
		&integrations,
		&sources,
		&detail.SupportsColumnLineage,
	)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to summarize an openlineage dataset")
	}
	if matched == 0 {
		return nil, false, nil
	}
	if lastSeen.Valid {
		t := lastSeen.Time
		detail.LastSeen = &t
	}
	detail.Integrations = []string(integrations)
	detail.Sources = []string(sources)
	return detail, true, nil
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

// ListOpenLineageDatasetFilterValues returns the dataset dimensions the
// OpenLineage filter menus offer, most common first. An integration or a source
// counts once per run that carried the dataset, which is what the dataset list
// itself displays.
func (s *Store) ListOpenLineageDatasetFilterValues(ctx context.Context) ([]*OpenLineageFilterValue, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT '`+openLineageFilterDatasetNamespace+`', namespace, count(*) FROM openlineage_run_dataset WHERE namespace <> '' GROUP BY 2
		UNION ALL
		SELECT '`+openLineageFilterDatasetIntegration+`', integration, count(DISTINCT run_pk) FROM openlineage_run_dataset WHERE integration <> '' GROUP BY 2
		UNION ALL
		SELECT '`+openLineageFilterDatasetSource+`', source, count(DISTINCT run_pk) FROM openlineage_run_dataset WHERE source <> '' GROUP BY 2
		ORDER BY 1, 3 DESC, 2`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list openlineage dataset filter values")
	}
	return collectOpenLineageFilterValues(rows)
}

// openLineageDatasetPairClause renders the predicate selecting exactly the
// given (namespace, name) spellings. The pairs are bound positionally so a
// namespace of one pair cannot combine with the name of another.
func openLineageDatasetPairClause(pairs []OpenLineageDatasetPair, startIndex int) (string, []any) {
	clauses := make([]string, 0, len(pairs))
	args := make([]any, 0, len(pairs)*2)
	for _, pair := range pairs {
		clauses = append(clauses, fmt.Sprintf(
			"(d.namespace = $%d AND d.name = $%d)",
			startIndex+len(args)+1, startIndex+len(args)+2,
		))
		args = append(args, pair.Namespace, pair.Name)
	}
	return strings.Join(clauses, " OR "), args
}
