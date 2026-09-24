package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedBodiesAreFresh regenerates the TiDB and MariaDB bodies from the
// MySQL source and compares them with the files on disk. It is what replaced the
// old copy-detector: drift can no longer be reported after the fact, because the
// generated files cannot be edited into disagreement in the first place.
func TestGeneratedBodiesAreFresh(t *testing.T) {
	t.Parallel()

	outputs, err := Generate("..")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(outputs) != len(dialects) {
		t.Fatalf("generated %d files, want one per dialect (%d)", len(outputs), len(dialects))
	}
	for _, output := range outputs {
		existing, err := os.ReadFile(output.Path)
		if err != nil {
			t.Fatalf("read %s: %v", output.Path, err)
		}
		if !bytes.Equal(existing, output.Content) {
			t.Errorf("%s is stale; run go generate ./backend/plugin/lineage/mysql", output.Path)
		}
	}
}

// TestGeneratedBodiesCopyTheSourceVerbatim proves the generated files carry the
// MySQL body byte for byte. That is what "one dialect cannot change alone" means
// here, and it is the property a formatter would silently break.
func TestGeneratedBodiesCopyTheSourceVerbatim(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", sourceFile))
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	body, err := Split(src)
	if err != nil {
		t.Fatalf("split source: %v", err)
	}
	for _, d := range dialects {
		generated, err := os.ReadFile(filepath.Join("..", "..", d.name, generatedFile))
		if err != nil {
			t.Fatalf("read generated %s: %v", d.name, err)
		}
		if !bytes.Contains(generated, body) {
			t.Errorf("%s does not contain the MySQL body byte for byte", filepath.Join(d.name, generatedFile))
		}
	}
}

// TestSplitRejectsMalformedSource pins the two ways the source file can stop
// carrying a usable seam, and that the split drops the blank line between the
// sentinel and the body.
func TestSplitRejectsMalformedSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "no sentinel",
			src:  "package mysql\n\nfunc f() {}\n",
			want: "exactly once",
		},
		{
			name: "sentinel twice",
			src:  "package mysql\n\n" + Sentinel + "\n\na\n\n" + Sentinel + "\n\nb\n",
			want: "exactly once",
		},
		{
			name: "nothing below the sentinel",
			src:  "package mysql\n\n" + Sentinel + "\n",
			want: "nothing below the sentinel",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Split([]byte(test.src)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Split error = %v, want one containing %q", err, test.want)
			}
		})
	}

	body, err := Split([]byte("package mysql\n\nconst a = 1\n\n" + Sentinel + "\n\nfunc f() {}\n"))
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if got, want := string(body), "func f() {}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// TestSeamSelfCheck keeps the generation-time check honest: a dialect header that
// stops declaring a hook, or a body that stops using one, has to fail generation
// rather than compile into a dialect that quietly ignores a variation point.
func TestSeamSelfCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, headerFile)
	write := func(content string) {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write header: %v", err)
		}
	}

	write("package tidb\n\nfunc valuesQueryPrimary() {}\nfunc rowAliasNames() {}\n")
	err := checkSeam(dir, map[string]bool{"resultTableName": true})
	if err == nil || !strings.Contains(err.Error(), "does not declare resultTableName") {
		t.Fatalf("checkSeam error = %v, want one naming the undeclared constant", err)
	}

	referenced := map[string]bool{}
	for _, name := range seamIdentifiers {
		referenced[name] = true
	}
	write("package tidb\n\nconst (\n\tresultTableName = 1\n\tdeletionFieldName = 2\n\twildcardColumn = 3\n\tfileSourceMarker = 4\n)\n\nfunc valuesQueryPrimary() {}\nfunc rowAliasNames() {}\n")
	if err := checkSeam(dir, referenced); err != nil {
		t.Fatalf("checkSeam on a complete header: %v", err)
	}

	delete(referenced, "fileSourceMarker")
	err = checkSeam(dir, referenced)
	if err == nil || !strings.Contains(err.Error(), "no longer uses fileSourceMarker") {
		t.Fatalf("checkSeam error = %v, want one naming the unused constant", err)
	}
}
