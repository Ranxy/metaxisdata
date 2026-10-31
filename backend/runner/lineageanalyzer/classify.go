package lineageanalyzer

import (
	"context"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/dmltarget"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// classifyApplicationSQL decides whether a reported statement is a read or a write, and
// returns the graph edge a write with no column relation still needs.
//
// The rule, and why it is not simply "the analysis produced a relation": the analyzers
// record column-level derivation, so a statement that reads a column produces a relation
// whose target is the statement, while a statement that writes values from nowhere — the
// ORM's `INSERT INTO t (a, b) VALUES (?, ?)`, or a `DELETE` that references no column —
// produces none at all in every dialect. Those are writes all the same, and without a target
// the graph would show nothing about them, so the target is read from the statement itself.
//
// The table-level edge carries no columns and no transformations, which is the shape the
// OpenLineage table-level path already stores. See
// .agents/docs/application-sql-ingestion.md, "Classification and the table-level edge".
func (a *Analyzer) classifyApplicationSQL(
	ctx context.Context,
	ac catalog.AnalysisContext,
	engine storepb.Engine,
	definition string,
	relations []model.ColumnRelation,
	metaGUID string,
) (store.ApplicationSQLKind, []*store.ColumnLineage) {
	for _, rel := range relations {
		if !rel.IsTemp {
			return store.ApplicationSQLKindWrite, nil
		}
	}

	targets, err := dmltarget.Targets(engine, sqlsanitize.Analyzable(engine, definition))
	if err != nil {
		// Not a write, or one this package cannot name. Either way it has no out-edge, and a
		// SELECT with no source column is not drawn at all — manual SQL has the same hole.
		return store.ApplicationSQLKindRead, nil
	}

	var edges []*store.ColumnLineage
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		// The analyzer produced no relation here, so the name is read from the statement and the
		// registry is asked for the object it belongs to: an engine that binds identifiers
		// case-insensitively may have written `orders` for the object the registry holds as
		// `Orders`, and an edge into a GUID the registry does not have points at nothing.
		id := a.lineage.CanonicalIdentifier(ctx, engine, ac.Complete(target))
		guid := id.GUID()
		if guid == metaGUID {
			continue
		}
		if _, ok := seen[guid]; ok {
			continue
		}
		seen[guid] = struct{}{}
		edges = append(edges, &store.ColumnLineage{
			MetaGUID:     metaGUID,
			MetaType:     storepb.MetaType_APPLICATION_SQL,
			SourceGUID:   metaGUID,
			SourceType:   storepb.MetaType_APPLICATION_SQL,
			TargetGUID:   guid,
			TargetColumn: "",
			// The column is not null in the schema, so an empty list is required rather than a
			// nil one; RelationTypeOf reads it as `direct`.
			Transformation: make([]model.Transformation, 0),
			RelationType:   model.RelationTypeOf(nil),
		})
	}
	if len(edges) == 0 {
		return store.ApplicationSQLKindRead, nil
	}
	return store.ApplicationSQLKindWrite, edges
}
