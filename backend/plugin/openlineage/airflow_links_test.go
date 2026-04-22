package openlineage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeriveAirflowLinks(t *testing.T) {
	rawPayload := []byte(`{
		"run": {
			"facets": {
				"airflow": {
					"taskInstance": {
						"log_url": "http://localhost:8080/dags/datax_mysql_to_pg/runs/manual__2026-04-21T08%3A34%3A29.072518%2B00%3A00/tasks/sync_testtable_to_t_table?try_number=1"
					}
				}
			}
		}
	}`)

	links := DeriveAirflowLinks(rawPayload)

	assert.Equal(t, "http://localhost:8080/dags/datax_mysql_to_pg", links.DagURL)
	assert.Equal(t, "http://localhost:8080/dags/datax_mysql_to_pg/runs/manual__2026-04-21T08%3A34%3A29.072518%2B00%3A00/tasks/sync_testtable_to_t_table?try_number=1", links.RunLogURL)
}

func TestDeriveAirflowLinksWithoutLogURL(t *testing.T) {
	links := DeriveAirflowLinks([]byte(`{"run":{"facets":{}}}`))

	assert.Empty(t, links.DagURL)
	assert.Empty(t, links.RunLogURL)
}
