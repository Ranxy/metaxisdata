package lineage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"testing"
)

// dialectPackages are the analyzer packages that walk their own AST but must not
// own the analysis mechanism itself.
var dialectPackages = []string{"postgresql", "mysql", "tidb", "mariadb", "starrocks"}

// forbiddenDeclarations are the pieces of analysis that used to exist once per
// dialect and drifted apart: the edge set and its key, the predicate influence
// bookkeeping, the name-based temporary-relation filtering and the set-operation
// merge. They live in `algorithm` (or `model`) now, and a dialect that declares
// one again is a dialect that is about to disagree with the others.
//
// This is the guard `dupl` cannot be: golangci-lint compares files within one
// package, and every copy here lives in a different package. Three byte-identical
// MySQL-family analyzers and two near-identical predicate implementations were
// invisible to it.
var forbiddenDeclarations = []string{
	"edgeKey",
	"columnEdgeKey",
	"predicateInfluence",
	"predicateKey",
	"EdgeSet",
	"Influences",
	"mergeSetOpOutputColumns",
	"mergeUnionOutputColumns",
	"combineTransformations",
	"isTempRelation",
	"markTempTable",
	"isTableTempInCurrentScope",
	"tempColumnNames",
	"attachTempColumnLookup",
	"exposedColumnNames",
	"resolveOutputColumns",
	"normalizeIdentifier",
	"wildcardSourceRef",
}

// requiredSharedDeclarations are the mechanisms every dialect builds on, by the
// package that owns them. A shared package dropping one of them means the
// behavior it encodes was dropped too.
var requiredSharedDeclarations = map[string][]string{
	"algorithm": {
		"EdgeSet",
		"Influences",
		"MergeSetOpColumns",
		"ArmChain",
		"FlattenTempSources",
		"FlattenTempSourceLineage",
		"TraceThroughTableLineage",
		"TraceThroughTableLineageToTarget",
		"ResolveOutputColumns",
	},
	// What a relation exposes to name-based resolution: the columns of a
	// query-local relation, the list a CTE or derived table declares, and the
	// reference a wildcard contributes.
	"scope": {
		"TempColumnNames",
		"AttachTempColumnLookup",
		"ExposedColumnNames",
		"WildcardSourceRef",
	},
	"model": {
		"NormalizeIdentifier",
	},
}

// TestSharedAnalysisStaysShared keeps the dialect-neutral analysis in one place.
func TestSharedAnalysisStaysShared(t *testing.T) {
	t.Parallel()

	for pkg, names := range requiredSharedDeclarations {
		declared := declarations(t, pkg)
		for _, name := range names {
			if !declared[name] {
				t.Errorf("%s no longer declares %s; the dialects build on it", pkg, name)
			}
		}
	}

	for _, dialect := range dialectPackages {
		declared := declarations(t, dialect)
		for _, name := range forbiddenDeclarations {
			if declared[name] {
				t.Errorf("%s declares %s, which the shared analysis layer owns; "+
					"a second implementation is how the dialects drifted apart", dialect, name)
			}
		}
	}
}

// declarations returns the names one package declares, as functions, methods and
// types. Test files are left out: a test may legitimately name a helper after the
// mechanism it exercises.
func declarations(t *testing.T, pkg string) map[string]bool {
	t.Helper()

	dir := filepath.Join(".", pkg)
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	slices.Sort(files)

	out := make(map[string]bool)
	for _, file := range files {
		if filepath.Ext(file) != ".go" || len(file) > 8 && file[len(file)-8:] == "_test.go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				out[d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						out[typeSpec.Name.Name] = true
					}
				}
			default:
				// Only functions and types carry a mechanism.
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no declarations found in %s", dir)
	}
	return out
}
