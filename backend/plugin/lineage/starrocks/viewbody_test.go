package starrocks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExtractViewDDL pins the workaround for omni's CREATE VIEW grammar gap
// (finding F2): a body the parser rejects is split off the statement so it can
// be parsed as a top-level query.
func TestExtractViewDDL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sql     string
		schema  string
		target  string
		columns []string
		body    string
		ok      bool
	}{
		{
			name:   "CTE body",
			sql:    "CREATE VIEW `v` AS WITH c AS (SELECT id FROM t) SELECT id FROM c",
			target: "v",
			body:   "WITH c AS (SELECT id FROM t) SELECT id FROM c",
			ok:     true,
		},
		{
			name:    "set-operation body with column list and database qualifier",
			sql:     "CREATE OR REPLACE VIEW db1.v (x, y) AS SELECT id, name FROM t1 UNION ALL SELECT id, name FROM t2",
			schema:  "db1",
			target:  "v",
			columns: []string{"x", "y"},
			body:    "SELECT id, name FROM t1 UNION ALL SELECT id, name FROM t2",
			ok:      true,
		},
		{
			name:   "parenthesized body",
			sql:    "CREATE VIEW v AS (SELECT id FROM t1)",
			target: "v",
			body:   "(SELECT id FROM t1)",
			ok:     true,
		},
		{
			name: "full SHOW CREATE MATERIALIZED VIEW output",
			sql: "CREATE MATERIALIZED VIEW `mv1` (`id`, `cnt`)\n" +
				"DISTRIBUTED BY HASH(`id`)\n" +
				"REFRESH ASYNC\n" +
				"PROPERTIES (\"replication_num\" = \"1\")\n" +
				"AS SELECT id, count(*) AS cnt FROM t1 GROUP BY id",
			target:  "mv1",
			columns: []string{"id", "cnt"},
			body:    "SELECT id, count(*) AS cnt FROM t1 GROUP BY id",
			ok:      true,
		},
		{
			name:    "SECURITY NONE is skipped",
			sql:     "CREATE VIEW v (a) SECURITY NONE AS SELECT 1",
			target:  "v",
			columns: []string{"a"},
			body:    "SELECT 1",
			ok:      true,
		},
		{
			name:   "COMMENT is skipped",
			sql:    "CREATE VIEW v COMMENT 'view comment' AS SELECT 1",
			target: "v",
			body:   "SELECT 1",
			ok:     true,
		},
		{
			name:   "ALTER VIEW",
			sql:    "ALTER VIEW v AS SELECT id FROM t1 UNION ALL SELECT id FROM t2",
			target: "v",
			body:   "SELECT id FROM t1 UNION ALL SELECT id FROM t2",
			ok:     true,
		},
		{name: "not a view statement", sql: "CREATE TABLE t3 AS SELECT id FROM t"},
		{name: "plain query", sql: "SELECT id FROM t"},
		{name: "no AS", sql: "CREATE VIEW v SELECT id"},
		{name: "no name", sql: "CREATE VIEW AS SELECT 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ddl, ok := extractViewDDL(tt.sql)
			require.Equal(t, tt.ok, ok)
			if !tt.ok {
				require.Nil(t, ddl)
				return
			}
			require.NotNil(t, ddl)
			require.Equal(t, tt.schema, ddl.schema)
			require.Equal(t, tt.target, ddl.name)
			require.Equal(t, tt.columns, ddl.columns)
			require.Equal(t, tt.body, ddl.body)
		})
	}
}

// CREATE TABLE ... LIKE copies a table's shape without reading it, so it
// carries no column lineage.
func TestCreateTableLikeHasNoLineage(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("CREATE TABLE t3 LIKE t1", nil)
	require.NoError(t, err)
	require.Empty(t, relations)
}
