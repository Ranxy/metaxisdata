package openlineage

import (
	"github.com/pkg/errors"
)

// The per-event caps. A request body limit alone leaves a single event free to
// be as large as the body, and every list view that later reads the run would
// pay for it; these keep one event, and the identity it is indexed under, small.
const (
	// MaxEventSize bounds one event rather than one request: a batch body may
	// be several megabytes as long as each event in it fits here.
	MaxEventSize = 1 << 20
	// The identity caps keep the unique btree indexes of openlineage_run and
	// openlineage_task below PostgreSQL's index item size. A value longer than
	// the cap used to be stored and then fail the insert forever, so the event
	// never became retryable; rejecting it up front makes the failure the
	// producer's to fix.
	MaxJobNamespaceLength = 255
	MaxJobNameLength      = 255
	MaxJobTypeLength      = 64
	MaxRunIDLength        = 255
	// Dataset names are longer than job names by nature: they can carry a
	// database, a schema and a table, and BigQuery prefixes a project.
	MaxDatasetNamespaceLength = 255
	MaxDatasetNameLength      = 512
	// MaxEventDatasets bounds the references one event may declare, and with
	// them the ingestion work and stored rows one run costs.
	MaxEventDatasets = 1000
	// MaxGUIDLength bounds the escaped GUID a run or task is indexed under.
	// PostgreSQL's btree item limit is about 2704 bytes, and the per-field caps
	// above can approach it once every character needs percent-encoding.
	MaxGUIDLength = 2000
	// The remaining strings an event writes into the run row. The run and task
	// lists read them for every row of a page, so an uncapped one would let one
	// page read an unbounded number of bytes even though raw_payload is not
	// selected, and these columns are what a producer can put in a single event.
	MaxEventTypeLength          = 64
	MaxProducerLength           = 512
	MaxIntegrationLength        = 64
	MaxProcessingTypeLength     = 64
	MaxParentJobNamespaceLength = 255
	MaxParentJobNameLength      = 255
	MaxParentRunIDLength        = 255
	MaxRootJobNamespaceLength   = 255
	MaxRootJobNameLength        = 255
	MaxRootRunIDLength          = 255
	// MaxColumnNameLength bounds the columnLineage strings. They reach the
	// indexed source_column/target_column of column_lineage, and a dataset a
	// facet names without listing it as an input or output builds a GUID from
	// its name, so both are capped like the identity fields.
	MaxColumnNameLength = 512
	// MaxAirflowRunLogURLLength bounds the link stored on the run. It is derived
	// from the event with url.String(), which percent-encodes, so the stored link
	// can be longer than the facet it came from; past this the link is dropped
	// rather than stored and read back for every row of a page.
	MaxAirflowRunLogURLLength = 2048
)

// ErrEventTooLarge reports an event beyond MaxEventSize. The ingestion handler
// answers it with 413 rather than the 400 a malformed event earns.
var ErrEventTooLarge = errors.New("openlineage event exceeds the size limit")

// ValidateEventLimits enforces the per-event caps on a parsed event. RawJSON is
// set by ParseRunEvent, which is the only supported way to build one.
func ValidateEventLimits(event *RunEvent) error {
	if len(event.RawJSON) > MaxEventSize {
		return errors.Wrapf(ErrEventTooLarge, "event is %d bytes, limit is %d bytes", len(event.RawJSON), MaxEventSize)
	}

	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"job.namespace", event.Job.Namespace, MaxJobNamespaceLength},
		{"job.name", event.Job.Name, MaxJobNameLength},
		{"run.runId", event.Run.RunID, MaxRunIDLength},
		{"eventType", event.EventType, MaxEventTypeLength},
	} {
		if len(field.value) > field.limit {
			return errors.Errorf("openlineage %s is %d bytes, limit is %d bytes", field.name, len(field.value), field.limit)
		}
	}

	derived := DeriveRunMetadata(event)
	if len(derived.JobType) > MaxJobTypeLength {
		return errors.Errorf("openlineage job type is %d bytes, limit is %d bytes", len(derived.JobType), MaxJobTypeLength)
	}
	if guid := BuildOpenLineageRunGUID(event.Job.Namespace, event.Job.Name, derived.JobType, event.Run.RunID); len(guid) > MaxGUIDLength {
		return errors.Errorf("openlineage run GUID is %d bytes, limit is %d bytes", len(guid), MaxGUIDLength)
	}

	// Everything else the run row stores from the event, so a page of the run or
	// task list cannot read an unbounded value out of one row.
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"producer", event.Producer, MaxProducerLength},
		{"integration", derived.Integration, MaxIntegrationLength},
		{"processing type", derived.ProcessingType, MaxProcessingTypeLength},
		{"parent job namespace", derived.ParentJobNamespace, MaxParentJobNamespaceLength},
		{"parent job name", derived.ParentJobName, MaxParentJobNameLength},
		{"parent run id", derived.ParentRunID, MaxParentRunIDLength},
		{"root job namespace", derived.RootJobNamespace, MaxRootJobNamespaceLength},
		{"root job name", derived.RootJobName, MaxRootJobNameLength},
		{"root run id", derived.RootRunID, MaxRootRunIDLength},
	} {
		if len(field.value) > field.limit {
			return errors.Errorf("openlineage %s is %d bytes, limit is %d bytes", field.name, len(field.value), field.limit)
		}
	}

	if len(event.Inputs)+len(event.Outputs) > MaxEventDatasets {
		return errors.Errorf("openlineage event names %d datasets, limit is %d", len(event.Inputs)+len(event.Outputs), MaxEventDatasets)
	}
	for _, datasets := range [][]Dataset{event.Inputs, event.Outputs} {
		for _, dataset := range datasets {
			if len(dataset.Namespace) > MaxDatasetNamespaceLength {
				return errors.Errorf("openlineage dataset namespace is %d bytes, limit is %d bytes", len(dataset.Namespace), MaxDatasetNamespaceLength)
			}
			if len(dataset.Name) > MaxDatasetNameLength {
				return errors.Errorf("openlineage dataset name is %d bytes, limit is %d bytes", len(dataset.Name), MaxDatasetNameLength)
			}
			if err := validateColumnLineageFacet(dataset.Facets.ColumnLineage); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateColumnLineageFacet caps the strings a columnLineage facet contributes.
// Its field names become indexed column_lineage columns, and the datasets it
// references resolve into GUIDs, so an over-long one would otherwise fail an
// insert after the run row was already committed — a delivery the producer can
// only retry forever.
func validateColumnLineageFacet(facet *ColumnLineageFacet) error {
	if facet == nil {
		return nil
	}
	for field, lineage := range facet.Fields {
		if err := validateFieldLength("columnLineage field", field, MaxColumnNameLength); err != nil {
			return err
		}
		for _, input := range lineage.InputFields {
			if err := validateFieldLength("columnLineage input field", input.Field, MaxColumnNameLength); err != nil {
				return err
			}
			if err := validateFieldLength("columnLineage input namespace", input.Namespace, MaxDatasetNamespaceLength); err != nil {
				return err
			}
			if err := validateFieldLength("columnLineage input name", input.Name, MaxDatasetNameLength); err != nil {
				return err
			}
		}
	}
	for _, reference := range facet.Dataset {
		if err := validateFieldLength("columnLineage dataset field", reference.Field, MaxColumnNameLength); err != nil {
			return err
		}
		if err := validateFieldLength("columnLineage dataset namespace", reference.Namespace, MaxDatasetNamespaceLength); err != nil {
			return err
		}
		if err := validateFieldLength("columnLineage dataset name", reference.Name, MaxDatasetNameLength); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldLength(name, value string, limit int) error {
	if len(value) > limit {
		return errors.Errorf("openlineage %s is %d bytes, limit is %d bytes", name, len(value), limit)
	}
	return nil
}
