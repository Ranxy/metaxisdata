package openlineage

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxReportedNames bounds how many names one event contributes to a log line.
const maxReportedNames = 5

// lineageReport records what registry validation did to one event's edges.
type lineageReport struct {
	keptEdges            int
	degradedEdges        int
	invalidColumns       []string
	unknownRelationEdges int
	unknownRelations     []string
}

func (r *lineageReport) addInvalidColumn(reference string) {
	if len(r.invalidColumns) < maxReportedNames {
		r.invalidColumns = append(r.invalidColumns, reference)
	}
}

func (r *lineageReport) addUnknownRelation(guid string) {
	if len(r.unknownRelations) >= maxReportedNames || slices.Contains(r.unknownRelations, guid) {
		return
	}
	r.unknownRelations = append(r.unknownRelations, guid)
}

// log reports what validation did. Both cases are disagreements between the
// ingested event and the metadata registry, so both are warnings: an edge the
// registry proves wrong is dropped, and a relation the registry does not have
// yet is kept but named.
func (r lineageReport) log(event *RunEvent) {
	if r.degradedEdges > 0 {
		slog.Warn("blanked ingested lineage columns their relation does not have, keeping the table-level edge",
			"jobNamespace", event.Job.Namespace,
			"jobName", event.Job.Name,
			"runId", event.Run.RunID,
			"degradedEdges", r.degradedEdges,
			"keptEdges", r.keptEdges,
			"columns", r.invalidColumns,
		)
	}
	if r.unknownRelationEdges > 0 {
		slog.Warn("ingested lineage referenced relations the metadata registry does not have",
			"jobNamespace", event.Job.Namespace,
			"jobName", event.Job.Name,
			"runId", event.Run.RunID,
			"edges", r.unknownRelationEdges,
			"relations", r.unknownRelations,
		)
	}
}

type lineageEndpoint struct {
	guid   string
	column string
}

func (e lineageEndpoint) isInternal() bool {
	return e.guid != "" && !IsExternalGUID(e.guid)
}

func endpointsOf(edge *store.ColumnLineage) [2]lineageEndpoint {
	return [2]lineageEndpoint{
		{guid: edge.SourceGUID, column: edge.SourceColumn},
		{guid: edge.TargetGUID, column: edge.TargetColumn},
	}
}

// filterIngestedLineage removes the column claims the metadata registry proves
// wrong while keeping the dependency they were attached to.
//
// A claim is dropped when its relation is a known object that models columns as
// registry rows and the column it names is not one of them - that is where the
// positional placeholders and the cross-product columns came from. The edge
// itself survives as a table-level edge, because the run really did read that
// relation and write the other one: dropping it would lose a true dependency
// (the SQL extractor reports the dependency only through such a claim). A
// relation the registry does not know yet is left intact and reported, since
// ingestion runs before the next schema sync and the event is only processed
// once.
func filterIngestedLineage(
	lineages []*store.ColumnLineage,
	relations map[string]storepb.MetaType,
	columns map[string]struct{},
) ([]*store.ColumnLineage, lineageReport) {
	kept := make([]*store.ColumnLineage, 0, len(lineages))
	seen := make(map[string]struct{}, len(lineages))
	report := lineageReport{}

	for _, edge := range lineages {
		for _, endpoint := range endpointsOf(edge) {
			if !endpoint.isInternal() {
				continue
			}
			if _, known := relations[endpoint.guid]; !known {
				report.unknownRelationEdges++
				report.addUnknownRelation(endpoint.guid)
			}
		}

		adjusted := *edge
		sourceHolds := columnClaimHolds(adjusted.SourceGUID, adjusted.SourceColumn, relations, columns)
		targetHolds := columnClaimHolds(adjusted.TargetGUID, adjusted.TargetColumn, relations, columns)
		if !sourceHolds || !targetHolds {
			if !sourceHolds {
				report.addInvalidColumn(columnGUID(adjusted.SourceGUID, adjusted.SourceColumn))
			}
			if !targetHolds {
				report.addInvalidColumn(columnGUID(adjusted.TargetGUID, adjusted.TargetColumn))
			}
			// Once one side of the mapping is unverifiable the mapping as a whole
			// is, so the edge becomes the table-level edge the run really implies:
			// no columns, no transformations, no relation type derived from them.
			adjusted.SourceColumn = ""
			adjusted.TargetColumn = ""
			adjusted.Transformation = []model.Transformation{}
			adjusted.RelationType = model.RelationTypeOf(nil)
			report.degradedEdges++
		}

		key := strings.Join([]string{adjusted.SourceGUID, adjusted.SourceColumn, adjusted.TargetGUID, adjusted.TargetColumn}, "\x00")
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, &adjusted)
	}

	report.keptEdges = len(kept)
	return kept, report
}

// columnClaimHolds reports whether the registry contradicts a column claim. A
// claim holds when there is no column to check, when the endpoint is external,
// when the relation is not known yet, or when the relation stores its columns
// somewhere other than in registry rows (a view keeps them in its own metadata).
func columnClaimHolds(
	guid, column string,
	relations map[string]storepb.MetaType,
	columns map[string]struct{},
) bool {
	if column == "" || guid == "" || IsExternalGUID(guid) {
		return true
	}
	objectType, known := relations[guid]
	if !known || !modelsColumnsAsObjects(objectType) {
		return true
	}
	_, ok := columns[columnGUID(guid, column)]
	return ok
}

// modelsColumnsAsObjects reports whether a relation's columns exist as their
// own registry rows. Only a TABLE does: a view or materialized view keeps its
// columns inside its own metadata, so a column claim against one of those is
// left alone rather than dropped for a row that was never written.
func modelsColumnsAsObjects(objectType storepb.MetaType) bool {
	return objectType == storepb.MetaType_TABLE
}

// columnGUID is the registry GUID of a column. common.BuildMetaGUID would escape
// the separators already inside the relation GUID, so the column name is escaped
// and appended the way the schema syncer builds a column GUID.
func columnGUID(relationGUID, column string) string {
	return relationGUID + common.MetaGUIDSplit + common.EscapeGUIDPart(column)
}

// collectRelationGUIDs returns the distinct internal relations an edge set
// references, which is the set to look up in the registry.
func collectRelationGUIDs(lineages []*store.ColumnLineage) []string {
	seen := make(map[string]struct{}, len(lineages)*2)
	guids := make([]string, 0, len(lineages)*2)
	for _, edge := range lineages {
		for _, endpoint := range endpointsOf(edge) {
			if !endpoint.isInternal() {
				continue
			}
			if _, ok := seen[endpoint.guid]; ok {
				continue
			}
			seen[endpoint.guid] = struct{}{}
			guids = append(guids, endpoint.guid)
		}
	}
	return guids
}

// collectColumnGUIDs returns the distinct column GUIDs worth looking up: a column
// claim is only checkable when its relation is known and models columns as rows.
func collectColumnGUIDs(lineages []*store.ColumnLineage, relations map[string]storepb.MetaType) []string {
	seen := make(map[string]struct{}, len(lineages)*2)
	guids := make([]string, 0, len(lineages)*2)
	for _, edge := range lineages {
		for _, endpoint := range endpointsOf(edge) {
			if !endpoint.isInternal() || endpoint.column == "" {
				continue
			}
			objectType, known := relations[endpoint.guid]
			if !known || !modelsColumnsAsObjects(objectType) {
				continue
			}
			guid := columnGUID(endpoint.guid, endpoint.column)
			if _, ok := seen[guid]; ok {
				continue
			}
			seen[guid] = struct{}{}
			guids = append(guids, guid)
		}
	}
	return guids
}

// validateIngestedLineage checks the edges an event is about to contribute
// against the metadata registry and returns the subset worth storing.
func (p *Processor) validateIngestedLineage(
	ctx context.Context,
	lineages []*store.ColumnLineage,
) ([]*store.ColumnLineage, lineageReport, error) {
	relations, err := p.existingRelations(ctx, collectRelationGUIDs(lineages))
	if err != nil {
		return nil, lineageReport{}, errors.Wrap(err, "failed to look up the relations an OpenLineage event referenced")
	}
	columns, err := p.existingGUIDs(ctx, collectColumnGUIDs(lineages, relations))
	if err != nil {
		return nil, lineageReport{}, errors.Wrap(err, "failed to look up the columns an OpenLineage event referenced")
	}

	kept, report := filterIngestedLineage(lineages, relations, columns)
	return kept, report, nil
}

// existingRelations returns the object type of every guid the metadata registry
// holds, for the guids it holds them for.
func (p *Processor) existingRelations(ctx context.Context, guids []string) (map[string]storepb.MetaType, error) {
	existing := make(map[string]storepb.MetaType, len(guids))
	if len(guids) == 0 {
		return existing, nil
	}
	resources, err := p.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{GUIDs: &guids})
	if err != nil {
		return nil, err
	}
	for _, resource := range resources {
		existing[resource.GUID] = resource.ObjectType
	}
	return existing, nil
}

// existingGUIDs returns the subset of guids the metadata registry holds. It
// reads digests only: validation needs existence, never the metadata itself.
func (p *Processor) existingGUIDs(ctx context.Context, guids []string) (map[string]struct{}, error) {
	existing := make(map[string]struct{}, len(guids))
	if len(guids) == 0 {
		return existing, nil
	}
	resources, err := p.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{GUIDs: &guids})
	if err != nil {
		return nil, err
	}
	for _, resource := range resources {
		existing[resource.GUID] = struct{}{}
	}
	return existing, nil
}
