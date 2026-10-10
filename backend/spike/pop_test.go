package spike

import (
	"context"
	"fmt"
	"testing"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestPopulationBoundary(t *testing.T) {
	cases := []struct {
		engine storepb.Engine
		sql    string
	}{
		{storepb.Engine_MYSQL, "DELETE FROM t"},
		{storepb.Engine_MYSQL, "DELETE FROM t WHERE 1 = 1"},
		{storepb.Engine_MYSQL, "UPDATE t SET a = DEFAULT"},
		{storepb.Engine_MYSQL, "TRUNCATE TABLE t"},
		{storepb.Engine_MYSQL, "INSERT INTO t DEFAULT VALUES"},
		{storepb.Engine_MYSQL, "INSERT INTO t (a) VALUES (NULL)"},
		{storepb.Engine_POSTGRES, "DELETE FROM t"},
		{storepb.Engine_POSTGRES, "DELETE FROM t WHERE true"},
		{storepb.Engine_POSTGRES, "INSERT INTO t DEFAULT VALUES"},
		{storepb.Engine_POSTGRES, "UPDATE t SET a = DEFAULT"},
		{storepb.Engine_STARROCKS, "DELETE FROM t"},
		{storepb.Engine_STARROCKS, "INSERT INTO t DEFAULT VALUES"},
	}
	for _, c := range cases {
		rels, err := analyzer.Analyze(context.Background(), c.engine, c.sql)
		nonTemp := 0
		for _, r := range rels {
			if !r.IsTemp {
				nonTemp++
			}
		}
		target := "?"
		if got, e := DMLTargets(c.engine, c.sql); e == nil {
			target = fmt.Sprint(got)
		}
		fmt.Printf("[%-9s] relations=%d nonTemp=%d d9target=%-8s err=%v  %s\n", c.engine, len(rels), nonTemp, target, err != nil, c.sql)
	}
}
