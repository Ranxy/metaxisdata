package openlineage

import (
	"encoding/json"
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

// Ingestion stores the whitelisted run log URL on the run row and a reader
// rebuilds the links from it, so the two paths must agree: the list pages no
// longer hold the payload the link was derived from.
func TestAirflowLinksFromRunLogURLMatchesThePayloadDerivation(t *testing.T) {
	rawPayload := []byte(`{"run":{"facets":{"airflow":{"taskInstance":{"log_url":"  https://airflow.example.com/dags/x/runs/1  "}}}}}`)

	fromPayload := DeriveAirflowLinks(rawPayload)
	fromColumn := AirflowLinksFromRunLogURL(fromPayload.RunLogURL)
	assert.Equal(t, fromPayload, fromColumn)
	assert.Equal(t, "https://airflow.example.com/dags/x", fromColumn.DagURL)
	assert.Equal(t, "https://airflow.example.com/dags/x/runs/1", fromColumn.RunLogURL)

	// A URL without the `/runs/` marker has no DAG link, but it is still the run
	// log link the Airflow facet advertised.
	withoutMarker := AirflowLinksFromRunLogURL("https://airflow.example.com/tree?dag_id=x")
	assert.Empty(t, withoutMarker.DagURL)
	assert.Equal(t, "https://airflow.example.com/tree?dag_id=x", withoutMarker.RunLogURL)

	// The whitelist applies on read as well, so a stored value that is not a web
	// URL cannot become a link even if it reaches the column.
	assert.Empty(t, AirflowLinksFromRunLogURL("javascript:alert(1)").RunLogURL)
}

// The facet is caller-supplied, so only a real web address may reach a link.
// A `javascript:` URL would otherwise run in the origin of the member who
// clicked "open run log".
func TestDeriveAirflowLinksRejectsNonWebURLs(t *testing.T) {
	cases := map[string]string{
		"javascript scheme":        "javascript:alert(document.cookie)",
		"javascript mixed case":    "JaVaScRiPt:alert(1)",
		"data scheme":              "data:text/html,<script>alert(1)</script>",
		"file scheme":              "file:///etc/passwd",
		"protocol relative":        "//evil.example.com/dags/x/runs/1",
		"relative path":            "/dags/x/runs/1",
		"no host":                  "http:///dags/x/runs/1",
		"port only":                "http://:8080/dags/x/runs/1",
		"scheme without host":      "https:",
		"empty after trimming":     "   ",
		"leading whitespace only":  "\n\t",
		"control character in URL": "http://localhost:8080/dags/x/runs/\x01",
	}

	for name, logURL := range cases {
		t.Run(name, func(t *testing.T) {
			rawPayload := []byte(`{"run":{"facets":{"airflow":{"taskInstance":{"log_url":` + mustJSONString(t, logURL) + `}}}}}`)

			links := DeriveAirflowLinks(rawPayload)

			assert.Empty(t, links.RunLogURL)
			assert.Empty(t, links.DagURL)
		})
	}
}

// A URL that already is a web address is kept, normalized, and its Dag URL is
// still derived; the scheme is not case-sensitive.
func TestDeriveAirflowLinksKeepsWebURLs(t *testing.T) {
	cases := map[string]struct {
		logURL  string
		wantURL string
		wantDag string
	}{
		"uppercase scheme": {
			logURL:  "HTTPS://airflow.example.com/dags/x/runs/1",
			wantURL: "https://airflow.example.com/dags/x/runs/1",
			wantDag: "https://airflow.example.com/dags/x",
		},
		"padded": {
			logURL:  "  http://airflow.example.com/dags/x/runs/1  ",
			wantURL: "http://airflow.example.com/dags/x/runs/1",
			wantDag: "http://airflow.example.com/dags/x",
		},
		"credentials": {
			logURL:  "http://user:pass@airflow.example.com/dags/x/runs/1",
			wantURL: "http://user:pass@airflow.example.com/dags/x/runs/1",
			wantDag: "http://user:pass@airflow.example.com/dags/x",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rawPayload := []byte(`{"run":{"facets":{"airflow":{"taskInstance":{"log_url":` + mustJSONString(t, tc.logURL) + `}}}}}`)

			links := DeriveAirflowLinks(rawPayload)

			assert.Equal(t, tc.wantURL, links.RunLogURL)
			assert.Equal(t, tc.wantDag, links.DagURL)
		})
	}
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	assert.NoError(t, err)
	return string(encoded)
}
