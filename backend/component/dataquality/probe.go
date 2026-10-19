package dataquality

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ProbeStatement wraps a compiled statement so the target engine parses and plans
// it without returning any row. It is what a dry run executes: it proves the
// syntax, the column references and the query permission, but it must not scan
// the table.
//
// The wrapper is per engine because "return nothing" is spelled differently:
// LIMIT 0 on the MySQL family and PostgreSQL, TOP 0 on SQL Server.
func ProbeStatement(engine storepb.Engine, query CompiledQuery) string {
	// A metadata rule compiles to no statement (schema_drift reads the registry),
	// so there is nothing to probe: the dry run for it validates the target and the
	// config only. The empty string tells the caller to skip the probe.
	if query.Kind == QueryMetadata || query.SQL == "" {
		return ""
	}
	// The subquery alias is required by MySQL; the other engines accept it too.
	const alias = " AS dq"
	switch engine {
	case storepb.Engine_MSSQL:
		return "SELECT TOP 0 * FROM (" + query.SQL + ")" + alias
	default:
		return "SELECT * FROM (" + query.SQL + ")" + alias + " LIMIT 0"
	}
}
