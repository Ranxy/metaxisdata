package store

import (
	"context"
	"database/sql"

	"github.com/pkg/errors"
)

// The dimensions the OpenLineage index pages filter by, as the query below
// labels them. A value's dimension is what tells the caller which menu it
// belongs to.
const (
	openLineageFilterJobNamespace = "job_namespace"
	openLineageFilterJobType      = "job_type"
	openLineageFilterEventType    = "event_type"
	openLineageFilterSource       = "source"

	// The dimensions only the dataset page offers, read from the dataset
	// references rather than from the runs' raw payloads.
	openLineageFilterDatasetNamespace   = "dataset_namespace"
	openLineageFilterDatasetIntegration = "dataset_integration"
	openLineageFilterDatasetSource      = "dataset_source"
)

// maxOpenLineageFilterValues bounds each dimension: a workspace with a long tail
// of namespaces must not turn a filter menu into a second listing of its own.
const maxOpenLineageFilterValues = 200

// OpenLineageFilterValue is one distinct value a filter menu offers, with the
// number of rows carrying it.
type OpenLineageFilterValue struct {
	Dimension string
	Value     string
	Count     int64
}

// OpenLineageTotals are the registry-wide counts the index pages' summary cards
// show. They are counted rather than summed over the capped filter lists.
type OpenLineageTotals struct {
	Runs          int64
	Jobs          int64
	JobNamespaces int64
}

// CountOpenLineageTotals returns the totals the OpenLineage index pages summarise.
func (s *Store) CountOpenLineageTotals(ctx context.Context) (*OpenLineageTotals, error) {
	totals := &OpenLineageTotals{}
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM openlineage_run),
			(SELECT count(*) FROM openlineage_task),
			(SELECT count(DISTINCT job_namespace) FROM openlineage_run WHERE job_namespace <> '')`,
	).Scan(&totals.Runs, &totals.Jobs, &totals.JobNamespaces)
	if err != nil {
		return nil, errors.Wrap(err, "failed to count openlineage totals")
	}
	return totals, nil
}

// ListOpenLineageFilterValues returns the distinct values of every dimension the
// OpenLineage index pages filter by, most common first.
//
// Both tables are read in one statement so a menu costs one round trip. The
// dataset dimensions are answered separately by
// ListOpenLineageDatasetFilterValues: they describe the datasets of the window
// the dataset list itself reads.
func (s *Store) ListOpenLineageFilterValues(ctx context.Context) ([]*OpenLineageFilterValue, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT '`+openLineageFilterJobNamespace+`', job_namespace, count(*) FROM openlineage_run WHERE job_namespace <> '' GROUP BY 2
		UNION ALL
		SELECT '`+openLineageFilterJobType+`', job_type, count(*) FROM openlineage_task WHERE job_type <> '' GROUP BY 2
		UNION ALL
		SELECT '`+openLineageFilterEventType+`', event_type, count(*) FROM openlineage_run WHERE event_type <> '' GROUP BY 2
		UNION ALL
		SELECT '`+openLineageFilterSource+`', source, count(*) FROM openlineage_run WHERE source <> '' GROUP BY 2
		ORDER BY 1, 3 DESC, 2`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list openlineage filter values")
	}
	return collectOpenLineageFilterValues(rows)
}

// collectOpenLineageFilterValues caps each dimension. The rows arrive most
// common first within each dimension, so the cap keeps the values that matter
// and drops only the tail.
func collectOpenLineageFilterValues(rows *sql.Rows) ([]*OpenLineageFilterValue, error) {
	defer rows.Close()

	kept := make(map[string]int, 4)
	result := make([]*OpenLineageFilterValue, 0, 4*maxOpenLineageFilterValues)
	for rows.Next() {
		var value OpenLineageFilterValue
		if err := rows.Scan(&value.Dimension, &value.Value, &value.Count); err != nil {
			return nil, errors.Wrap(err, "failed to scan an openlineage filter value")
		}
		if kept[value.Dimension] >= maxOpenLineageFilterValues {
			continue
		}
		kept[value.Dimension]++
		result = append(result, &value)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read openlineage filter values")
	}
	return result, nil
}
