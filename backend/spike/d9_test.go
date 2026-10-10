package spike

import (
	"context"
	"fmt"
	"strings"
	"testing"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestD9TriggerPopulation measures the extractor on the only population it will
// ever run for: statements the analyzer answers with zero relations, and which are
// nevertheless writes. Everything the analyzer already answers is reported as out
// of population rather than as a failure — D9 is never consulted for it.
func TestD9TriggerPopulation(_ *testing.T) {
	cases := []struct {
		engine storepb.Engine
		sql    string
		want   string
	}{
		// The plain trigger: the ORM row insert, one row and many.
		{storepb.Engine_MYSQL, "INSERT INTO t (a, b) VALUES (1, 2)", "t"},
		{storepb.Engine_MYSQL, "INSERT INTO t (a, b) VALUES (1, 2), (3, 4), (5, 6)", "t"},
		{storepb.Engine_MYSQL, "INSERT IGNORE INTO t (a) VALUES (1)", "t"},
		{storepb.Engine_MYSQL, "REPLACE INTO t (a) VALUES (1)", "t"},
		{storepb.Engine_MYSQL, "INSERT INTO db1.t (a) VALUES (1)", "db1.t"},
		{storepb.Engine_MYSQL, "INSERT INTO t SET a = 1, b = 2", "t"},
		{storepb.Engine_MYSQL, "INSERT INTO t (a) VALUES (1) ON DUPLICATE KEY UPDATE a = 2", "t"},
		{storepb.Engine_MYSQL, "INSERT INTO `t` (`a`) VALUES (1)", "t"},
		{storepb.Engine_MYSQL, "INSERT INTO t (a) VALUES (NOW())", "t"},
		{storepb.Engine_MYSQL, "INSERT INTO t (a) VALUES (DEFAULT)", "t"},
		{storepb.Engine_MYSQL, "INSERT INTO t (a) SELECT 1", "t"},
		{storepb.Engine_POSTGRES, "INSERT INTO t (a, b) VALUES (1, 2)", "t"},
		{storepb.Engine_POSTGRES, "INSERT INTO t (a, b) VALUES (1, 2), (3, 4)", "t"},
		{storepb.Engine_POSTGRES, `INSERT INTO "public"."t" ("a") VALUES (1)`, "public.t"},
		{storepb.Engine_POSTGRES, "INSERT INTO t VALUES (1) ON CONFLICT (a) DO NOTHING", "t"},
		{storepb.Engine_POSTGRES, "INSERT INTO t (a) SELECT 1", "t"},
		{storepb.Engine_STARROCKS, "INSERT INTO t (a, b) VALUES (1, 2)", "t"},
		{storepb.Engine_STARROCKS, "INSERT INTO t VALUES (1), (2)", "t"},

		// A DELETE that references no column also yields zero relations in every
		// dialect, so DELETE is in the population too — not just INSERT.
		{storepb.Engine_MYSQL, "DELETE FROM t", "t"},
		{storepb.Engine_MYSQL, "DELETE FROM t WHERE 1 = 1", "t"},
		{storepb.Engine_MYSQL, "DELETE FROM db1.t", "db1.t"},
		{storepb.Engine_POSTGRES, "DELETE FROM t", "t"},
		{storepb.Engine_POSTGRES, "DELETE FROM t WHERE true", "t"},
		{storepb.Engine_POSTGRES, "INSERT INTO t DEFAULT VALUES", "t"},
		{storepb.Engine_STARROCKS, "DELETE FROM t", "t"},

		// Boundary: these are expected to be answered by the analyzer already.
		{storepb.Engine_MYSQL, "UPDATE t SET a = 1, b = 2", "t"},
		{storepb.Engine_MYSQL, "DELETE FROM t WHERE a = 1", "t"},
		{storepb.Engine_MYSQL, "UPDATE orders o JOIN users u ON o.user_id = u.id SET o.user_name = u.name", "orders"},
		{storepb.Engine_MYSQL, "UPDATE orders o JOIN users u ON o.user_id = u.id SET o.discount = 0.1", "orders"},
		{storepb.Engine_MYSQL, "DELETE o FROM orders o JOIN users u ON o.user_id = u.id WHERE u.status = 'deleted'", "orders"},
	}

	var inPopulation, ok, bad, outOfPopulation int
	for _, c := range cases {
		relations, err := analyzer.Analyze(context.Background(), c.engine, c.sql)
		if err != nil {
			fmt.Printf("[%-9s] NOT-ANALYZABLE %s -> %v\n", c.engine, oneLine(c.sql), firstLine(err))
			continue
		}
		if len(relations) > 0 {
			outOfPopulation++
			nonTemp := 0
			for _, rel := range relations {
				if !rel.IsTemp {
					nonTemp++
				}
			}
			fmt.Printf("[%-9s] OUT   relations=%d nonTemp=%d  %s\n", c.engine, len(relations), nonTemp, oneLine(c.sql))
			continue
		}
		inPopulation++
		got, err := DMLTargets(c.engine, c.sql)
		status := "no target"
		if err == nil {
			status = strings.Join(got, ",")
		}
		if err == nil && len(got) == 1 && lastSegment(got[0]) == lastSegment(c.want) {
			ok++
			fmt.Printf("[%-9s] OK    zero relations, target=%-10s want=%-10s %s\n", c.engine, status, c.want, oneLine(c.sql))
			continue
		}
		bad++
		fmt.Printf("[%-9s] BAD   zero relations, target=%-10s want=%-10s %s\n", c.engine, status, c.want, oneLine(c.sql))
	}
	fmt.Printf("\n### D9: %d cases reached the extractor (%d ok, %d bad); %d never reach it (the analyzer already names the target)\n",
		inPopulation, ok, bad, outOfPopulation)
}

func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i+1:]
	}
	return s
}
