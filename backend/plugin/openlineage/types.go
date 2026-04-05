package openlineage

import "encoding/json"

// RunEvent represents an OpenLineage RunEvent.
// See: https://openlineage.io/docs/spec/run-event
type RunEvent struct {
	EventType string          `json:"eventType"`
	EventTime string          `json:"eventTime"`
	Run       Run             `json:"run"`
	Job       Job             `json:"job"`
	Inputs    []Dataset       `json:"inputs"`
	Outputs   []Dataset       `json:"outputs"`
	Producer  string          `json:"producer"`
	RawJSON   json.RawMessage `json:"-"`
}

// Run represents the run entity in an OpenLineage event.
type Run struct {
	RunID  string                     `json:"runId"`
	Facets map[string]json.RawMessage `json:"facets,omitempty"`
}

// Job represents the job entity in an OpenLineage event.
type Job struct {
	Namespace string                     `json:"namespace"`
	Name      string                     `json:"name"`
	Facets    map[string]json.RawMessage `json:"facets,omitempty"`
}

// Dataset represents an input or output dataset in an OpenLineage event.
type Dataset struct {
	Namespace string        `json:"namespace"`
	Name      string        `json:"name"`
	Facets    DatasetFacets `json:"facets,omitempty"`
}

// DatasetFacets holds the standard facets attached to a dataset.
type DatasetFacets struct {
	Schema        *SchemaFacet        `json:"schema,omitempty"`
	ColumnLineage *ColumnLineageFacet `json:"columnLineage,omitempty"`

	// Additional (non-standard) facets stored as raw JSON.
	Additional map[string]json.RawMessage `json:"-"`
}

// SchemaFacet represents the OpenLineage SchemaDatasetFacet.
type SchemaFacet struct {
	Producer  string        `json:"_producer,omitempty"`
	SchemaURL string        `json:"_schemaURL,omitempty"`
	Fields    []SchemaField `json:"fields"`
}

// SchemaField represents a field in the schema facet.
type SchemaField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// ColumnLineageFacet represents the OpenLineage ColumnLineageDatasetFacet.
type ColumnLineageFacet struct {
	Producer  string                          `json:"_producer,omitempty"`
	SchemaURL string                          `json:"_schemaURL,omitempty"`
	Fields    map[string]ColumnLineageField   `json:"fields"`
	Dataset   []ColumnLineageDatasetReference `json:"dataset,omitempty"`
}

// ColumnLineageField represents column-level lineage for a single output column.
type ColumnLineageField struct {
	InputFields []InputField `json:"inputFields"`
}

// InputField identifies an input column contributing to an output column.
type InputField struct {
	Namespace       string        `json:"namespace"`
	Name            string        `json:"name"`
	Field           string        `json:"field"`
	Transformations []OLTransform `json:"transformations,omitempty"`
}

// OLTransform describes a transformation applied in OpenLineage.
type OLTransform struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	Description string `json:"description,omitempty"`
	Masking     bool   `json:"masking,omitempty"`
}

// ColumnLineageDatasetReference holds dataset-level lineage info.
type ColumnLineageDatasetReference struct {
	Namespace       string        `json:"namespace"`
	Name            string        `json:"name"`
	Field           string        `json:"field"`
	Transformations []OLTransform `json:"transformations,omitempty"`
}
