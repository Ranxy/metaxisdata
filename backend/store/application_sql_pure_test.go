package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildApplicationSQLGUID(t *testing.T) {
	t.Parallel()

	guid := buildApplicationSQLGUID("instance_1", "db1", "public", "abc123")
	require.Equal(t, "instance_1;db1;public;__application_sql__/abc123", guid)
	// The statement segment must keep its prefix so it cannot collide with a schema object
	// of the same name.
	require.NotEqual(t, guid, buildApplicationSQLGUID("instance_1", "db1", "public", "orders"))
}

func TestStatementLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single line", "SELECT a FROM t", "SELECT a FROM t"},
		// The label is the whole statement on one line: an application's formatting is not
		// what identifies it, and line one alone names a multi-line statement badly.
		{"lines are joined", "SELECT a\nFROM t\nWHERE b = ?", "SELECT a FROM t WHERE b = ?"},
		{"runs of space collapse", "SELECT  a\n\t\tFROM   t", "SELECT a FROM t"},
		{"surrounding space is trimmed", "  SELECT a FROM t  ", "SELECT a FROM t"},
		{"a leading comment does not hide the statement", "\n\nSELECT a\nFROM t", "SELECT a FROM t"},
		{
			"a CTE is named by the whole statement, not by its first line",
			"WITH recent AS (\n    SELECT id\n    FROM t\n)\nINSERT INTO audit (id)\nSELECT id FROM recent",
			"WITH recent AS ( SELECT id FROM t ) INSERT INTO audit (id) SELECT id FROM recent",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, statementLabel(tc.in))
		})
	}

	// Truncation counts runes, not bytes, so a multi-byte statement is not cut mid-character.
	long := strings.Repeat("查", applicationSQLLabelRunes+10)
	label := statementLabel(long)
	require.Equal(t, applicationSQLLabelRunes+1, len([]rune(label)), "the ellipsis is the extra rune")
	require.True(t, strings.HasSuffix(label, "…"))
}

func TestBuildApplicationSQLStoredMetadata(t *testing.T) {
	t.Parallel()

	msg := &ApplicationSQLMessage{
		ApplicationSQLID:   "abc123",
		InstanceResourceID: "instance_1",
		DatabaseName:       "db1",
		SchemaName:         "",
		SQLText:            "INSERT INTO orders (id, total) VALUES (?, ?)",
	}
	meta := buildApplicationSQLStoredMetadata(msg)
	appSQL := meta.GetApplicationSqlMetadata()
	require.NotNil(t, appSQL)
	require.Equal(t, "abc123", appSQL.GetApplicationSqlId())
	require.Equal(t, msg.SQLText, appSQL.GetSqlText())
	require.Equal(t, "INSERT INTO orders (id, total) VALUES (?, ?)", appSQL.GetName())
	// The mirror names the instance the way every other resource does.
	require.Equal(t, "instances/instance_1", appSQL.GetInstanceResource())
	require.Equal(t, "db1", appSQL.GetDatabaseName())
}

// TestUpsertKeepsTheStatementIdentityOnConflict pins the one thing the statement upsert must
// not do: rewrite the identifier its GUID was built from. Rewriting it would detach the
// object from its lineage and history, which is exactly what a second application reporting
// the same statement would otherwise cause.
func TestUpsertKeepsTheStatementIdentityOnConflict(t *testing.T) {
	t.Parallel()

	const conflictTarget = "ON CONFLICT (instance_resource_id, database_name, schema_name, fingerprint) DO UPDATE SET"
	_, afterConflict, found := strings.Cut(applicationSQLUpsertStatement, conflictTarget)
	require.True(t, found, "the conflict target is the statement's scope plus its fingerprint")

	onConflict, _, found := strings.Cut(afterConflict, "RETURNING")
	require.True(t, found)
	for _, column := range []string{"guid =", "application_sql_id =", "sql_text =", "fingerprint ="} {
		require.NotContains(t, onConflict, column, "a conflict must not rewrite %s", column)
	}
	require.Contains(t, onConflict, "LEAST(application_sql.first_seen_at", "the first sighting is kept")
	require.Contains(t, onConflict, "GREATEST(application_sql.last_seen_at", "the last sighting is kept")
}

// TestObservationUpsertAddsToTheCount pins the frequency semantics: a report adds to what an
// application has already reported, it does not replace it.
func TestObservationUpsertAddsToTheCount(t *testing.T) {
	t.Parallel()

	require.Contains(t, applicationSQLUpsertObservation, "ON CONFLICT (application_sql_id, application_id) DO UPDATE SET")
	require.Contains(t, applicationSQLUpsertObservation, "execution_count = application_sql_observation.execution_count + EXCLUDED.execution_count")
	require.Contains(t, applicationSQLUpsertObservation, "LEAST(application_sql_observation.first_seen_at")
	require.Contains(t, applicationSQLUpsertObservation, "GREATEST(application_sql_observation.last_seen_at")
}

// TestRollUpDerivesFromObservations pins that the statement's totals are recomputed from the
// observations rather than taken from the caller's number.
func TestRollUpDerivesFromObservations(t *testing.T) {
	t.Parallel()

	require.Contains(t, applicationSQLRollUpStatement, "FROM application_sql_observation WHERE application_sql_id = $1")
	require.Contains(t, applicationSQLRollUpStatement, "SUM(execution_count)")
	require.Contains(t, applicationSQLRollUpStatement, "MIN(first_seen_at)")
	require.Contains(t, applicationSQLRollUpStatement, "MAX(last_seen_at)")
}

// TestDeleteOrphansRequiresNoObservation pins the cleanup rule: a statement goes only once no
// application reports it any more.
func TestDeleteOrphansRequiresNoObservation(t *testing.T) {
	t.Parallel()

	require.Contains(t, applicationSQLDeleteOrphans, "NOT EXISTS")
	require.Contains(t, applicationSQLDeleteOrphans, "o.application_sql_id = application_sql.id")
	require.Contains(t, applicationSQLDeleteOrphans, "RETURNING guid",
		"the caller needs the GUIDs to clean the mirror and the lineage")
}

func TestApplicationSQLKindValuesAreStored(t *testing.T) {
	t.Parallel()

	// The column's SQL comment names these numbers, so they are part of the schema rather
	// than an internal detail: renumbering them needs a migration.
	require.Equal(t, ApplicationSQLKind(0), ApplicationSQLKindUnknown)
	require.Equal(t, ApplicationSQLKind(1), ApplicationSQLKindRead)
	require.Equal(t, ApplicationSQLKind(2), ApplicationSQLKindWrite)
}
