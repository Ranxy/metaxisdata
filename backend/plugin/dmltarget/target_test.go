package dmltarget_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/dmltarget"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
)

var analyzer = lineage.NewAnalyzer(nil, engines.Registrations()...)

// TestTargetsCoversEveryZeroRelationWrite pins the shapes this package is asked about
// and nothing else. Each is a statement whose analysis comes back empty, so the
// extractor is the only thing that names the table being written.
func TestTargetsCoversEveryZeroRelationWrite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		engine  storepb.Engine
		sql     string
		want    string
		dialect bool
	}{
		{"mysql single row", storepb.Engine_MYSQL, "INSERT INTO t (a, b) VALUES (1, 2)", "t", false},
		{"mysql many rows", storepb.Engine_MYSQL, "INSERT INTO t (a, b) VALUES (1, 2), (3, 4), (5, 6)", "t", false},
		{"mysql insert ignore", storepb.Engine_MYSQL, "INSERT IGNORE INTO t (a) VALUES (1)", "t", false},
		{"mysql replace", storepb.Engine_MYSQL, "REPLACE INTO t (a) VALUES (1)", "t", false},
		{"mysql qualified", storepb.Engine_MYSQL, "INSERT INTO db1.t (a) VALUES (1)", "db1.t", false},
		{"mysql insert set", storepb.Engine_MYSQL, "INSERT INTO t SET a = 1, b = 2", "t", false},
		{"mysql on duplicate key", storepb.Engine_MYSQL, "INSERT INTO t (a) VALUES (1) ON DUPLICATE KEY UPDATE a = 2", "t", false},
		{"mysql quoted identifier", storepb.Engine_MYSQL, "INSERT INTO `t` (`a`) VALUES (1)", "t", false},
		{"mysql function value", storepb.Engine_MYSQL, "INSERT INTO t (a) VALUES (NOW())", "t", false},
		{"mysql default value", storepb.Engine_MYSQL, "INSERT INTO t (a) VALUES (DEFAULT)", "t", false},
		{"mysql constant select", storepb.Engine_MYSQL, "INSERT INTO t (a) SELECT 1", "t", false},
		{"mysql bare delete", storepb.Engine_MYSQL, "DELETE FROM t", "t", false},
		{"mysql constant delete", storepb.Engine_MYSQL, "DELETE FROM t WHERE 1 = 1", "t", false},
		{"mysql qualified delete", storepb.Engine_MYSQL, "DELETE FROM db1.t", "db1.t", false},
		{"postgres single row", storepb.Engine_POSTGRES, "INSERT INTO t (a, b) VALUES (1, 2)", "t", false},
		{"postgres many rows", storepb.Engine_POSTGRES, "INSERT INTO t (a, b) VALUES (1, 2), (3, 4)", "t", false},
		{"postgres qualified", storepb.Engine_POSTGRES, `INSERT INTO "public"."t" ("a") VALUES (1)`, "public.t", false},
		{"postgres on conflict", storepb.Engine_POSTGRES, "INSERT INTO t VALUES (1) ON CONFLICT (a) DO NOTHING", "t", false},
		{"postgres default values", storepb.Engine_POSTGRES, "INSERT INTO t DEFAULT VALUES", "t", false},
		{"postgres bare delete", storepb.Engine_POSTGRES, "DELETE FROM t", "t", false},
		{"postgres constant delete", storepb.Engine_POSTGRES, "DELETE FROM t WHERE true", "t", false},
		{"starrocks single row", storepb.Engine_STARROCKS, "INSERT INTO t (a, b) VALUES (1, 2)", "t", false},
		{"starrocks many rows", storepb.Engine_STARROCKS, "INSERT INTO t VALUES (1), (2)", "t", false},
		{"starrocks bare delete", storepb.Engine_STARROCKS, "DELETE FROM t", "t", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := dmltarget.Targets(c.engine, c.sql)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, c.want, got[0].FullName())
			// A statement never names the instance; Complete fills it in.
			require.Empty(t, got[0].InstanceID)
		})
	}
}

// TestTargetsIsOnlyAskedWhenTheAnalysisIsEmpty pins the boundary from the other side:
// every shape the package refuses is one the analyzer already answers, so asking about
// it would be redundant rather than wrong. It also keeps the narrow population honest —
// if an analyzer change ever makes one of these yield no relation, this test fails
// rather than the gap going unnoticed.
func TestTargetsIsOnlyAskedWhenTheAnalysisIsEmpty(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		engine storepb.Engine
		sql    string
	}{
		{"constant assignment", storepb.Engine_MYSQL, "UPDATE t SET a = 1, b = 2"},
		{"delete by column", storepb.Engine_MYSQL, "DELETE FROM t WHERE a = 1"},
		{"update over a join", storepb.Engine_MYSQL, "UPDATE orders o JOIN users u ON o.user_id = u.id SET o.user_name = u.name"},
		{"constant assignment over a join", storepb.Engine_MYSQL, "UPDATE orders o JOIN users u ON o.user_id = u.id SET o.discount = 0.1"},
		{"multi-table delete", storepb.Engine_MYSQL, "DELETE o FROM orders o JOIN users u ON o.user_id = u.id WHERE u.status = 'deleted'"},
		{"create table as select", storepb.Engine_MYSQL, "CREATE TABLE summary AS SELECT category, COUNT(*) AS cnt FROM products GROUP BY category"},
		{"postgres update", storepb.Engine_POSTGRES, "UPDATE t SET a = 1 WHERE b = 2 AND c = 3"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			relations, err := analyzer.Analyze(context.Background(), c.engine, c.sql)
			require.NoError(t, err, c.sql)
			require.NotEmpty(t, relations,
				"this shape now yields no relation, so the extractor has to handle it: %s", c.sql)
		})
	}
}

// TestTargetsRefusesWhatItCannotName keeps the extractor from inventing a table. The
// statements it refuses divide in two: shapes that are not writes at all, and shapes
// whose target it cannot name without resolving aliases.
func TestTargetsRefusesWhatItCannotName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		engine storepb.Engine
		sql    string
	}{
		{storepb.Engine_MYSQL, "SELECT a FROM t"},
		{storepb.Engine_MYSQL, "CREATE TABLE t (a INT)"},
		{storepb.Engine_MYSQL, "TRUNCATE TABLE t"},
		// This form writes the DELETE list as aliases, so the only honest answer is to
		// refuse: `o` is not a table.
		{storepb.Engine_MYSQL, "DELETE o FROM orders o JOIN users u ON o.user_id = u.id"},
		{storepb.Engine_MYSQL, "DELETE FROM orders USING users WHERE orders.uid = users.id"},
		{storepb.Engine_POSTGRES, "SELECT a FROM t"},
		{storepb.Engine_STARROCKS, "SELECT a FROM t"},
	}

	for _, c := range cases {
		got, err := dmltarget.Targets(c.engine, c.sql)
		require.ErrorIs(t, err, dmltarget.ErrNoTarget, "%s %s", c.engine, c.sql)
		require.Nil(t, got)
	}
}
