package starrocks

import (
	starrocksparser "github.com/bytebase/omni/starrocks/parser"
)

// omni builds its token-name table on the first call to TokenName (`tokenNames` in
// starrocks/parser/keywords.go) without a lock, so two analyses reaching the view-body
// fallback at the same time race on it: `go test -race` reports it, and a concurrent map
// read and write panics. Calling it once here, at package initialization, builds the table
// before any analysis runs — every later call is then a read — which is what the table's own
// laziness was for.
//
// The doris dialect carries the same call for its own parser package. Recorded in
// .agents/docs/omni-upstream-defects.md §13; this can go when the upstream table is built at
// init or guarded.
// tokenNameTableIsBuilt records that this package built omni's token-name table at
// initialization, which is what tokenname.go exists for. tokenname_test.go asserts it,
// so removing the init is a red test rather than a silent re-opening of the race.
var tokenNameTableIsBuilt bool

func init() {
	starrocksparser.TokenName(0)
	tokenNameTableIsBuilt = true
}
