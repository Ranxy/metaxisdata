package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Sentinel is the line that separates the MySQL header from the body the whole
// family shares. It must appear exactly once, on a line of its own.
const Sentinel = "// == MYSQL-FAMILY SHARED BODY =="

const (
	// sourceFile is the single editable copy of the family traversal.
	sourceFile = "analyzer.go"
	// generatedFile is the name the shared body is written under in a sibling.
	generatedFile = "analyzer_body_gen.go"
	// headerFile is the per-dialect header a sibling keeps beside the body.
	headerFile = "dialect.go"
)

// dialect is one sibling package that consumes the shared body.
type dialect struct {
	// name is the package clause and the directory the body is written to.
	name string
	// astPath and parserPath are this dialect's omni parser packages. The body
	// reaches them through the fixed aliases nodes and mysqlparser, which is why
	// the import block cannot be derived by a general tool.
	astPath    string
	parserPath string
}

var dialects = []dialect{
	{name: "tidb", astPath: "github.com/bytebase/omni/tidb/ast", parserPath: "github.com/bytebase/omni/tidb/parser"},
	{name: "mariadb", astPath: "github.com/bytebase/omni/mariadb/ast", parserPath: "github.com/bytebase/omni/mariadb/parser"},
}

// importSpec is one import the body may need.
type importSpec struct {
	alias string
	path  string
}

// importGroups are the imports the shared body draws on, grouped the way Go code
// in this repository is written: standard library, third party, this repository,
// then the dialect's own parser. An import is emitted only when the body
// actually names its alias.
var importGroups = [][]importSpec{
	{
		{alias: "cmp", path: "cmp"},
		{alias: "context", path: "context"},
		{alias: "fmt", path: "fmt"},
		{alias: "reflect", path: "reflect"},
		{alias: "slices", path: "slices"},
		{alias: "strings", path: "strings"},
		{alias: "sync", path: "sync"},
	},
	{
		{alias: "errors", path: "github.com/pkg/errors"},
	},
	{
		{alias: "storepb", path: "github.com/Ranxy/metaxisdata/backend/generated-go/store"},
		{alias: "lineage", path: "github.com/Ranxy/metaxisdata/backend/plugin/lineage"},
		{alias: "algorithm", path: "github.com/Ranxy/metaxisdata/backend/plugin/lineage/algorithm"},
		{alias: "catalog", path: "github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"},
		{alias: "model", path: "github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"},
		{alias: "scope", path: "github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"},
	},
}

// seamIdentifiers are the names the shared body takes from the per-dialect
// header: the four marker constants and the two hooks. Generation checks them in
// both directions, so a variation point that nobody hooked out fails here rather
// than somewhere in a 2,300-line generated file.
var seamIdentifiers = []string{
	"resultTableName",
	"deletionFieldName",
	"wildcardColumn",
	"fileSourceMarker",
	"valuesQueryPrimary",
	"rowAliasNames",
}

// Output is one generated file, at a path relative to the working directory.
type Output struct {
	Path    string
	Content []byte
}

// Split returns the shared body of the family source file: everything below the
// sentinel, without the blank line that separates the two. The sentinel must
// appear exactly once, as a line of its own, and the body must not be empty.
func Split(src []byte) ([]byte, error) {
	marker := "\n" + Sentinel + "\n"
	if strings.Count(string(src), marker) != 1 {
		return nil, fmt.Errorf("%s must contain %q exactly once, on a line of its own", sourceFile, Sentinel)
	}
	index := strings.Index(string(src), marker)
	body := bytes.TrimPrefix(src[index+len(marker):], []byte("\n"))
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("%s has nothing below the sentinel", sourceFile)
	}
	return body, nil
}

// Generate renders every dialect's body from the source file in sourceDir, which
// is the mysql package directory.
func Generate(sourceDir string) ([]Output, error) {
	src, err := os.ReadFile(filepath.Join(sourceDir, sourceFile))
	if err != nil {
		return nil, err
	}
	body, err := Split(src)
	if err != nil {
		return nil, err
	}
	referenced, err := identifiers(body)
	if err != nil {
		return nil, err
	}

	outputs := make([]Output, 0, len(dialects))
	for _, d := range dialects {
		dir := filepath.Join(sourceDir, "..", d.name)
		if err := checkSeam(dir, referenced); err != nil {
			return nil, err
		}
		content, err := Render(d, body)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, Output{Path: filepath.Join(dir, generatedFile), Content: content})
	}
	return outputs, nil
}

// Render returns the complete generated file for one dialect: the generated
// marker, the package clause, the imports the body uses and the body itself,
// unchanged.
func Render(d dialect, body []byte) ([]byte, error) {
	selectors, err := selectorNames(body)
	if err != nil {
		return nil, err
	}
	groups := make([][]importSpec, 0, len(importGroups)+1)
	groups = append(groups, importGroups...)
	groups = append(groups, []importSpec{
		{alias: "nodes", path: d.astPath},
		{alias: "mysqlparser", path: d.parserPath},
	})

	var b bytes.Buffer
	write := func(s string) {
		if _, err := b.WriteString(s); err != nil {
			panic(err)
		}
	}
	printf := func(format string, args ...any) {
		if _, err := fmt.Fprintf(&b, format, args...); err != nil {
			panic(err)
		}
	}

	write("// Code generated by go generate ./backend/plugin/lineage/mysql; DO NOT EDIT.\n\n")
	write("// The traversal in this file is the MySQL family's shared analyzer body, copied\n")
	write("// from backend/plugin/lineage/mysql/analyzer.go. Edit it there.\n\n")
	printf("package %s\n\nimport (\n", d.name)

	emitted := 0
	for _, group := range groups {
		written := 0
		for _, spec := range group {
			if !selectors[spec.alias] {
				continue
			}
			if written == 0 && emitted > 0 {
				write("\n")
			}
			emitted++
			written++
			if spec.alias == path.Base(spec.path) {
				printf("\t%q\n", spec.path)
				continue
			}
			printf("\t%s %q\n", spec.alias, spec.path)
		}
	}
	write(")\n\n")
	if _, err := b.Write(body); err != nil {
		panic(err)
	}

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format the %s body: %w", d.name, err)
	}
	return formatted, nil
}

// checkSeam fails when a dialect header does not declare every seam identifier,
// or when the shared body no longer uses one of them.
func checkSeam(dir string, referenced map[string]bool) error {
	declared, err := declarations(filepath.Join(dir, headerFile))
	if err != nil {
		return err
	}
	for _, name := range seamIdentifiers {
		if !declared[name] {
			return fmt.Errorf("%s/%s does not declare %s; the shared body needs it", dir, headerFile, name)
		}
		if !referenced[name] {
			return fmt.Errorf("the shared body no longer uses %s, so %s/%s should drop it", name, dir, headerFile)
		}
	}
	return nil
}

// parseBody parses the shared body as the body of a file, which is enough to tell
// how it is written without resolving anything.
func parseBody(body []byte) (*ast.File, error) {
	file, err := parser.ParseFile(token.NewFileSet(), generatedFile, "package generated\n\n"+string(body), parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse the shared body: %w", err)
	}
	return file, nil
}

// selectorNames returns the package-level names the body qualifies, which is what
// decides the generated import block.
func selectorNames(body []byte) (map[string]bool, error) {
	file, err := parseBody(body)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := selector.X.(*ast.Ident); ok {
			names[ident.Name] = true
		}
		return true
	})
	return names, nil
}

// identifiers returns every identifier the body names, including the ones it
// takes from the dialect header.
func identifiers(body []byte) (map[string]bool, error) {
	file, err := parseBody(body)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			names[ident.Name] = true
		}
		return true
	})
	return names, nil
}

// declarations returns the top-level names a Go file declares.
func declarations(file string) (map[string]bool, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			names[fn.Name.Name] = true
			continue
		}
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range genDecl.Specs {
			if typeSpec, ok := spec.(*ast.TypeSpec); ok {
				names[typeSpec.Name.Name] = true
			}
			if valueSpec, ok := spec.(*ast.ValueSpec); ok {
				for _, name := range valueSpec.Names {
					names[name.Name] = true
				}
			}
		}
	}
	return names, nil
}
