package mysql

import (
	"testing"

	mysqlparser "github.com/bytebase/omni/mysql/parser"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// BenchmarkAnalyzeCorpus measures end-to-end analysis (parse + lineage) over the
// whole golden corpus in one operation, giving a single throughput number for
// the analyzer.
func BenchmarkAnalyzeCorpus(b *testing.B) {
	testutil.BenchmarkCorpusAnalysis(b, testdataPath("analyze"), analyzeSQL)
}

// BenchmarkAnalyzeCase measures each corpus statement separately so the slow
// shapes are visible.
func BenchmarkAnalyzeCase(b *testing.B) {
	testutil.BenchmarkCaseAnalysis(b, testdataPath("analyze"), analyzeSQL)
}

// BenchmarkParseCase isolates the omni parse cost from the lineage walk, so the
// analyzer's own overhead is (AnalyzeCase - ParseCase).
func BenchmarkParseCase(b *testing.B) {
	testutil.BenchmarkCaseParse(b, testdataPath("analyze"), func(sql string) error {
		_, err := mysqlparser.Parse(sql)
		return err
	})
}

// BenchmarkAnalyzeShapes measures synthetic statements that scale a single
// dimension (column count, nesting depth, join count) to show how analysis cost
// grows.
func BenchmarkAnalyzeShapes(b *testing.B) {
	testutil.BenchmarkShapes(b, analyzeSQL)
}

// BenchmarkAnalyzeWildcardWithCatalog measures SELECT * expansion against a
// 100-column catalog, the production path where the metadata store is consulted.
func BenchmarkAnalyzeWildcardWithCatalog(b *testing.B) {
	testutil.BenchmarkWildcardCatalog(b, analyzeSQL)
}
