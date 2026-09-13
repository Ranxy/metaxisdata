package migrator

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/blang/semver/v4"
)

func TestGetVersionFromPath(t *testing.T) {
	tests := []struct {
		path    string
		want    string
		wantErr bool
	}{
		{"migration/0.1/0000##init.sql", "0.1.0", false},
		{"migration/0.1/0001##add_col.sql", "0.1.1", false},
		{"migration/0.2/0021##migrate_users.sql", "0.2.21", false},
		{"migration/LATEST.sql", "", true},          // not a versioned migration
		{"migration/0.1/add_col.sql", "", true},     // missing ## separator
		{"migration/0.1.sql", "", true},             // wrong depth
		{"migration/0.1/00ab##bad.sql", "", true},   // non-numeric patch
		{"migration/0.1/00001##wide.sql", "", true}, // patch must be exactly four digits
		{"migration/0.1/1##narrow.sql", "", true},   // patch must be exactly four digits
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := getVersionFromPath(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGetSortedVersionedFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"migration/LATEST.sql":          {Data: []byte("-- baseline")},
		"migration/0.1/0002##b.sql":     {Data: []byte("-- b")},
		"migration/0.1/0001##a.sql":     {Data: []byte("-- a")},
		"migration/0.1/0000##seed.sql":  {Data: []byte("-- seed")},
		"migration/0.0/0005##prior.sql": {Data: []byte("-- prior")},
	}

	files, err := getSortedVersionedFiles(fsys)
	if err != nil {
		t.Fatalf("getSortedVersionedFiles: %v", err)
	}

	want := []string{"0.0.5", "0.1.0", "0.1.1", "0.1.2"}
	if len(files) != len(want) {
		t.Fatalf("got %d files, want %d (%v)", len(files), len(want), files)
	}
	seen := make(map[string]bool)
	for i, f := range files {
		if f.version.String() != want[i] {
			t.Errorf("files[%d] = %s, want %s", i, f.version, want[i])
		}
		if seen[f.version.String()] {
			t.Errorf("duplicate version %s", f.version)
		}
		seen[f.version.String()] = true
	}
}

func TestGetSortedVersionedFilesExcludesLATEST(t *testing.T) {
	fsys := fstest.MapFS{
		"migration/LATEST.sql": {Data: []byte("-- baseline only")},
	}
	files, err := getSortedVersionedFiles(fsys)
	if err != nil {
		t.Fatalf("getSortedVersionedFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no versioned files, got %v", files)
	}
}

// Two files with the same version would both execute and the second ledger
// insert would fail after the first one's DDL already ran.
func TestGetSortedVersionedFilesRejectsDuplicateVersions(t *testing.T) {
	fsys := fstest.MapFS{
		"migration/LATEST.sql":      {Data: []byte("-- baseline")},
		"migration/0.1/0001##a.sql": {Data: []byte("-- a")},
		"migration/0.1/0001##b.sql": {Data: []byte("-- b")},
	}

	_, err := getSortedVersionedFiles(fsys)
	if err == nil {
		t.Fatal("expected a duplicate-version error")
	}
	if !strings.Contains(err.Error(), "duplicate migration version 0.1.1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestComputeLatestVersion(t *testing.T) {
	t.Run("empty falls back to baseline", func(t *testing.T) {
		got := computeLatestVersion(nil)
		if got.String() != baselineVersion {
			t.Fatalf("got %s, want %s", got, baselineVersion)
		}
	})

	t.Run("max of files", func(t *testing.T) {
		files := []versionedFile{
			{version: ptrVersion("0.1.0")},
			{version: ptrVersion("0.1.3")},
			{version: ptrVersion("0.1.2")},
		}
		got := computeLatestVersion(files)
		if got.String() != "0.1.3" {
			t.Fatalf("got %s, want 0.1.3", got)
		}
	})

	t.Run("files below baseline keep baseline", func(t *testing.T) {
		files := []versionedFile{
			{version: ptrVersion("0.0.5")},
		}
		got := computeLatestVersion(files)
		if got.String() != baselineVersion {
			t.Fatalf("got %s, want %s", got, baselineVersion)
		}
	})
}

func TestMigratorEmbedContainsLATEST(t *testing.T) {
	if _, err := migrationFS.ReadFile(latestSchemaFileName); err != nil {
		t.Fatalf("embedded %q not readable: %v", latestSchemaFileName, err)
	}
}

func TestBaselineVersionIsCurrentAppVersion(t *testing.T) {
	if baselineVersion != "0.1.0" {
		t.Fatalf("baselineVersion = %s, want 0.1.0 (the current application version)", baselineVersion)
	}
}

// TestSchemaMigrationHistoryInLATEST locks in the version ledger that
// LATEST.sql must create on fresh installs. The migrator reads it to decide
// which incrementals are pending, and adopts pre-framework databases from the
// same DDL held in schemaMigrationHistoryDDL.
func TestSchemaMigrationHistoryInLATEST(t *testing.T) {
	sql := latestSQL(t)

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS schema_migration_history",
		"version TEXT NOT NULL",
		"idx_schema_migration_history_unique_version",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("LATEST.sql missing schema migration history declaration: %q", want)
		}
	}
	// The Go adoption DDL and the cumulative schema must declare the same
	// ledger, or a legacy database would be adopted into a differently shaped
	// table than a fresh install gets.
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS schema_migration_history",
		"idx_schema_migration_history_unique_version",
	} {
		if !strings.Contains(schemaMigrationHistoryDDL, want) {
			t.Fatalf("schemaMigrationHistoryDDL missing %q", want)
		}
	}
}

// latestSQL loads the canonical cumulative schema file. It is the baseline
// applied to fresh installs by the migrator; the tests here guard its contents
// directly rather than executing it against a live database.
func latestSQL(t *testing.T) string {
	t.Helper()
	// This test file lives in backend/migrator/, so migration/LATEST.sql is a
	// relative path under it. go test runs with the package directory as the
	// working directory.
	bytes, err := os.ReadFile(latestSchemaFileName)
	if err != nil {
		t.Fatalf("read %s: %v", latestSchemaFileName, err)
	}
	return string(bytes)
}

func ptrVersion(v string) *semver.Version {
	s := semver.MustParse(v)
	return &s
}
