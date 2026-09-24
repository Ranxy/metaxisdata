// Command gen copies the shared MySQL-family analyzer body into the TiDB and
// MariaDB packages.
//
// The three analyzers differ only in their header: the package clause, the omni
// AST and parser import paths, the engine they register and two dialect hooks.
// The traversal below the sentinel in ../analyzer.go is identical, so it is
// kept in that one file and copied from it, instead of being maintained three
// times and checked for drift after the fact. Run
// `go generate ./backend/plugin/lineage/mysql` after editing the traversal.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "report stale generated files instead of rewriting them")
	flag.Parse()

	if err := run(*check); err != nil {
		report("%v\n", err)
		os.Exit(1)
	}
}

// run writes every dialect's body, or with check set only verifies that the
// files on disk are the ones this source produces.
func run(check bool) error {
	outputs, err := Generate(".")
	if err != nil {
		return err
	}

	var stale []string
	for _, output := range outputs {
		if !check {
			if err := os.WriteFile(output.Path, output.Content, 0o644); err != nil {
				return err
			}
			continue
		}
		existing, readErr := os.ReadFile(output.Path)
		if readErr != nil || !bytes.Equal(existing, output.Content) {
			stale = append(stale, output.Path)
		}
	}
	if len(stale) > 0 {
		return fmt.Errorf("%s is stale; run go generate ./backend/plugin/lineage/mysql", strings.Join(stale, ", "))
	}
	return nil
}

// report writes to stderr, failing loudly when even that does not work.
func report(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stderr, format, args...); err != nil {
		panic(err)
	}
}
