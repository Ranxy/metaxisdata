package migrator

import (
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/blang/semver/v4"
)

// Every file under migration/ except LATEST.sql is a versioned migration, and
// its name is the version. A stray or malformed name fails startup loudly
// instead of being silently skipped, so the walker is the parse contract.
func TestGetSortedVersionedFiles(t *testing.T) {
	t.Parallel()

	const latest = "migration/LATEST.sql"
	tests := []struct {
		name    string
		tree    fstest.MapFS
		want    []string
		wantErr bool
	}{
		{
			name: "orders incrementals and excludes the baseline",
			tree: fstest.MapFS{
				latest:                          {Data: []byte("-- baseline")},
				"migration/0.1/0002##b.sql":     {Data: []byte("-- b")},
				"migration/0.1/0001##a.sql":     {Data: []byte("-- a")},
				"migration/0.1/0000##seed.sql":  {Data: []byte("-- seed")},
				"migration/0.0/0005##prior.sql": {Data: []byte("-- prior")},
				"migration/0.2/0021##users.sql": {Data: []byte("-- users")},
			},
			want: []string{"0.0.5", "0.1.0", "0.1.1", "0.1.2", "0.2.21"},
		},
		{
			name: "a tree with only the baseline has no incrementals",
			tree: fstest.MapFS{latest: {Data: []byte("-- baseline only")}},
		},
		{
			name:    "a name without the version separator fails",
			tree:    fstest.MapFS{"migration/0.1/add_col.sql": {Data: []byte("-- ")}},
			wantErr: true,
		},
		{
			name:    "a file outside a version directory fails",
			tree:    fstest.MapFS{"migration/0.1.sql": {Data: []byte("-- ")}},
			wantErr: true,
		},
		{
			name:    "a non-numeric patch fails",
			tree:    fstest.MapFS{"migration/0.1/00ab##bad.sql": {Data: []byte("-- ")}},
			wantErr: true,
		},
		{
			name:    "a patch wider than four digits fails",
			tree:    fstest.MapFS{"migration/0.1/00001##wide.sql": {Data: []byte("-- ")}},
			wantErr: true,
		},
		{
			name:    "a patch narrower than four digits fails",
			tree:    fstest.MapFS{"migration/0.1/1##narrow.sql": {Data: []byte("-- ")}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files, err := getSortedVersionedFiles(tt.tree)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", files)
				}
				return
			}
			if err != nil {
				t.Fatalf("getSortedVersionedFiles: %v", err)
			}
			got := make([]string, 0, len(files))
			for _, f := range files {
				got = append(got, f.version.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got versions %v, want %v", got, tt.want)
			}
		})
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
	t.Parallel()

	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{name: "an empty tree falls back to the baseline", want: baselineVersion},
		{name: "the newest file wins", versions: []string{"0.1.0", "0.1.3", "0.1.2"}, want: "0.1.3"},
		{name: "files below the baseline keep the baseline", versions: []string{"0.0.5"}, want: baselineVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := make([]versionedFile, 0, len(tt.versions))
			for _, v := range tt.versions {
				files = append(files, versionedFile{version: ptrVersion(v)})
			}
			if got := computeLatestVersion(files); got.String() != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMigratorEmbedContainsLATEST(t *testing.T) {
	if _, err := migrationFS.ReadFile(latestSchemaFileName); err != nil {
		t.Fatalf("embedded %q not readable: %v", latestSchemaFileName, err)
	}
}

// A database that predates the migration framework is adopted at the version the
// framework landed at, then gets the pending incrementals on top. Moving this
// value would mis-record the legacy ledger and either replay or skip
// incrementals on existing deployments.
func TestBaselineVersionIsTheAdoptionFloor(t *testing.T) {
	if baselineVersion != "0.1.0" {
		t.Fatalf("baselineVersion = %s, want 0.1.0 (the version a pre-framework database is adopted at)", baselineVersion)
	}
}

// LATEST.sql creates the version ledger on fresh installs, and the migrator
// adopts pre-framework databases from schemaMigrationHistoryDDL. Both must
// declare the same table and unique index, or an adopted database would carry a
// differently shaped ledger than a fresh install.
func TestSchemaMigrationHistoryInLATEST(t *testing.T) {
	sources := map[string]string{
		"migration/LATEST.sql":      latestSQL(t),
		"schemaMigrationHistoryDDL": schemaMigrationHistoryDDL,
	}
	for name, source := range sources {
		for _, want := range []string{
			"CREATE TABLE IF NOT EXISTS schema_migration_history",
			"version TEXT NOT NULL",
			"idx_schema_migration_history_unique_version",
		} {
			if !strings.Contains(source, want) {
				t.Fatalf("%s missing schema migration history declaration: %q", name, want)
			}
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
