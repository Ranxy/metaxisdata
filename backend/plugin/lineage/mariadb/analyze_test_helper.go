package mariadb

import (
	"context"
	"path/filepath"
	"runtime"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// analyzeSQL adapts the analyzer to the shared lineage test harness.
func analyzeSQL(sql string, cat catalog.Provide) ([]model.ColumnRelation, error) {
	return NewAnalyzer(context.TODO(), sql, cat).AnalyzeRelations()
}

// dialectCorpusDir locates this dialect's own golden corpus.
func dialectCorpusDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata", "analyze")
}

// sharedCorpusDir locates the MySQL golden corpus. Its statements are
// dialect-neutral SQL that every MySQL-family analyzer must resolve identically,
// so reusing it keeps the dialects behaviorally in sync.
func sharedCorpusDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "mysql", "testdata", "analyze")
}
