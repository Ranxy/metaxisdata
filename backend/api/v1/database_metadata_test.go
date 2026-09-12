package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/schema"
)

func TestBuildDiffSummaryEmpty(t *testing.T) {
	t.Parallel()

	require.Equal(t, "No changes detected.", buildDiffSummary(&schema.MetadataDiff{}))
}

// A change in a category the summary forgot was reported as "No changes
// detected." even though the DDL contained it, so every category must show up.
func TestBuildDiffSummaryCoversEveryCategory(t *testing.T) {
	t.Parallel()

	diff := &schema.MetadataDiff{
		SchemaChanges:           []*schema.SchemaDiff{{Action: schema.MetadataDiffActionCreate}},
		TableChanges:            []*schema.TableDiff{{Action: schema.MetadataDiffActionAlter}},
		ViewChanges:             []*schema.ViewDiff{{Action: schema.MetadataDiffActionDrop}},
		MaterializedViewChanges: []*schema.MaterializedViewDiff{{Action: schema.MetadataDiffActionCreate}},
		FunctionChanges:         []*schema.FunctionDiff{{Action: schema.MetadataDiffActionCreate}},
		ProcedureChanges:        []*schema.ProcedureDiff{{Action: schema.MetadataDiffActionCreate}},
		SequenceChanges:         []*schema.SequenceDiff{{Action: schema.MetadataDiffActionCreate}},
		EnumTypeChanges:         []*schema.EnumTypeDiff{{Action: schema.MetadataDiffActionCreate}},
		ExtensionChanges:        []*schema.ExtensionDiff{{Action: schema.MetadataDiffActionCreate}},
		EventTriggerChanges:     []*schema.EventTriggerDiff{{Action: schema.MetadataDiffActionCreate}},
		EventChanges:            []*schema.EventDiff{{Action: schema.MetadataDiffActionCreate}},
		CommentChanges:          []*schema.CommentDiff{{Action: schema.MetadataDiffActionCreate}},
	}

	summary := buildDiffSummary(diff)
	for _, label := range []string{
		"Schemas", "Tables", "Views", "Materialized views", "Functions",
		"Procedures", "Sequences", "Enum types", "Extensions",
		"Event triggers", "Events", "Comments",
	} {
		require.Contains(t, summary, label+":", "summary is missing %s", label)
	}
	require.Contains(t, summary, "Tables: +0 created, ~1 modified, -0 dropped")
	require.Contains(t, summary, "Views: +0 created, ~0 modified, -1 dropped")
}

func TestCountDiffActions(t *testing.T) {
	t.Parallel()

	created, altered, dropped := countDiffActions([]*schema.TableDiff{
		{Action: schema.MetadataDiffActionCreate},
		{Action: schema.MetadataDiffActionCreate},
		{Action: schema.MetadataDiffActionAlter},
		{Action: schema.MetadataDiffActionDrop},
	}, func(c *schema.TableDiff) schema.MetadataDiffAction { return c.Action })

	require.Equal(t, 2, created)
	require.Equal(t, 1, altered)
	require.Equal(t, 1, dropped)
}
