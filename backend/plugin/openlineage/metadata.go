package openlineage

import (
	"encoding/json"
	"net/url"
	"strings"
)

const (
	openLineageRunGUIDPrefix  = "openlineage:run:"
	openLineageTaskGUIDPrefix = "openlineage:task:"
	emptyGUIDPart             = "_"
)

// DerivedRunMetadata is the normalized OpenLineage metadata extracted from raw facets.
type DerivedRunMetadata struct {
	TaskGUID           string
	JobType            string
	Integration        string
	ProcessingType     string
	ParentJobNamespace string
	ParentJobName      string
	ParentRunID        string
	RootJobNamespace   string
	RootJobName        string
	RootRunID          string
	HasLineage         bool
}

type jobTypeFacet struct {
	JobType        string `json:"jobType"`
	Integration    string `json:"integration"`
	ProcessingType string `json:"processingType"`
}

type parentRunFacet struct {
	Job  parentFacetJob `json:"job"`
	Run  parentFacetRun `json:"run"`
	Root struct {
		Job parentFacetJob `json:"job"`
		Run parentFacetRun `json:"run"`
	} `json:"root"`
}

type parentFacetJob struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type parentFacetRun struct {
	RunID string `json:"runId"`
}

// DeriveRunMetadata extracts normalized task/job information from an OpenLineage event.
func DeriveRunMetadata(event *RunEvent) DerivedRunMetadata {
	jobType := "UNSPECIFIED"
	integration := ""
	processingType := ""
	if raw, ok := event.Job.Facets["jobType"]; ok {
		var facet jobTypeFacet
		if err := json.Unmarshal(raw, &facet); err == nil {
			jobType = strings.TrimSpace(facet.JobType)
			if jobType == "" {
				jobType = "UNSPECIFIED"
			}
			integration = strings.TrimSpace(facet.Integration)
			processingType = strings.TrimSpace(facet.ProcessingType)
		}
	}

	derived := DerivedRunMetadata{
		TaskGUID:       BuildOpenLineageTaskGUID(event.Job.Namespace, event.Job.Name, jobType),
		JobType:        jobType,
		Integration:    integration,
		ProcessingType: processingType,
		HasLineage:     hasLineageSignal(event),
	}

	if raw, ok := event.Run.Facets["parent"]; ok {
		var facet parentRunFacet
		if err := json.Unmarshal(raw, &facet); err == nil {
			derived.ParentJobNamespace = strings.TrimSpace(facet.Job.Namespace)
			derived.ParentJobName = strings.TrimSpace(facet.Job.Name)
			derived.ParentRunID = strings.TrimSpace(facet.Run.RunID)
			derived.RootJobNamespace = strings.TrimSpace(facet.Root.Job.Namespace)
			derived.RootJobName = strings.TrimSpace(facet.Root.Job.Name)
			derived.RootRunID = strings.TrimSpace(facet.Root.Run.RunID)
		}
	}

	return derived
}

func hasLineageSignal(event *RunEvent) bool {
	if len(event.Inputs) > 0 || len(event.Outputs) > 0 {
		return true
	}
	for _, output := range event.Outputs {
		if output.Facets.ColumnLineage == nil {
			continue
		}
		if len(output.Facets.ColumnLineage.Fields) > 0 || len(output.Facets.ColumnLineage.Dataset) > 0 {
			return true
		}
	}
	return false
}

func buildOpenLineageScopedGUID(prefix string, parts ...string) string {
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			trimmed = emptyGUIDPart
		}
		escaped = append(escaped, url.PathEscape(trimmed))
	}
	return prefix + strings.Join(escaped, ":")
}

// BuildOpenLineageTaskGUID returns the stable GUID for an aggregated OpenLineage task/job.
func BuildOpenLineageTaskGUID(namespace, name, jobType string) string {
	return buildOpenLineageScopedGUID(openLineageTaskGUIDPrefix, jobType, namespace, name)
}

// BuildOpenLineageRunGUID returns the stable GUID for a persisted OpenLineage run.
func BuildOpenLineageRunGUID(namespace, name, jobType, runID string) string {
	return buildOpenLineageScopedGUID(openLineageRunGUIDPrefix, jobType, namespace, name, runID)
}
