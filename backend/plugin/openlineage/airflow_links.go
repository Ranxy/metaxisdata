package openlineage

import (
	"encoding/json"
	"net/url"
	"strings"
)

// AirflowLinks contains Airflow UI URLs derived from an OpenLineage raw payload.
type AirflowLinks struct {
	DagURL    string
	RunLogURL string
}

type airflowFacetEnvelope struct {
	Run struct {
		Facets map[string]json.RawMessage `json:"facets"`
	} `json:"run"`
}

type airflowRunFacet struct {
	TaskInstance struct {
		LogURL string `json:"log_url"`
	} `json:"taskInstance"`
}

// DeriveAirflowLinks extracts Airflow web URLs from the original OpenLineage payload.
func DeriveAirflowLinks(rawPayload []byte) AirflowLinks {
	if len(rawPayload) == 0 {
		return AirflowLinks{}
	}

	var envelope airflowFacetEnvelope
	if err := json.Unmarshal(rawPayload, &envelope); err != nil {
		return AirflowLinks{}
	}

	rawFacet, ok := envelope.Run.Facets["airflow"]
	if !ok {
		return AirflowLinks{}
	}

	var facet airflowRunFacet
	if err := json.Unmarshal(rawFacet, &facet); err != nil {
		return AirflowLinks{}
	}

	runLogURL := strings.TrimSpace(facet.TaskInstance.LogURL)
	if runLogURL == "" {
		return AirflowLinks{}
	}

	return AirflowLinks{
		DagURL:    deriveAirflowDagURL(runLogURL),
		RunLogURL: runLogURL,
	}
}

func deriveAirflowDagURL(runLogURL string) string {
	parsed, err := url.Parse(runLogURL)
	if err != nil {
		return ""
	}

	markerIndex := strings.Index(parsed.Path, "/runs/")
	if markerIndex == -1 {
		return ""
	}

	parsed.Path = strings.TrimRight(parsed.Path[:markerIndex], "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
