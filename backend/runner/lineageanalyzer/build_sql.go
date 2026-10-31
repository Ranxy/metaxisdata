package lineageanalyzer

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// buildSQL extracts the object definition and wraps it as needed for lineage parsing.
func buildSQL(name string, engine storepb.Engine, metaType storepb.MetaType, res *store.MetaRegistryResource) (definition string, wrapped string, err error) {
	switch metaType {
	case storepb.MetaType_VIEW:
		definition = res.Metadata.GetViewMetadata().GetDefinition()
	case storepb.MetaType_MATERIALIZED_VIEW:
		definition = res.Metadata.GetMaterializedViewMetadata().GetDefinition()
	case storepb.MetaType_MANUAL_SQL:
		definition = res.Metadata.GetManualSqlMetadata().GetSqlText()
	case storepb.MetaType_APPLICATION_SQL:
		definition = res.Metadata.GetApplicationSqlMetadata().GetSqlText()
	default:
		return "", "", errors.Errorf("unsupported meta type %v", metaType)
	}
	if definition == "" {
		return "", "", nil
	}
	if metaType == storepb.MetaType_MANUAL_SQL {
		return definition, definition, nil
	}
	if metaType == storepb.MetaType_APPLICATION_SQL {
		// The stored text is the parameterized one, and the MySQL family's grammar has no
		// production that accepts a placeholder, so the analyzer is handed the substituted
		// form. The stored text stays the definition: it is what the object is.
		return definition, sqlsanitize.Analyzable(engine, definition), nil
	}
	// A stored definition that is already a complete statement is used as-is.
	// StarRocks keeps the whole SHOW CREATE MATERIALIZED VIEW output in
	// MaterializedViewMetadata.Definition, so wrapping it in
	// "CREATE MATERIALIZED VIEW <name> AS ..." would produce
	// "... AS CREATE MATERIALIZED VIEW ...", which no parser accepts.
	if isCompleteStatement(definition) {
		return definition, definition, nil
	}

	keyword := "CREATE VIEW"
	if metaType == storepb.MetaType_MATERIALIZED_VIEW {
		keyword = "CREATE MATERIALIZED VIEW"
	}

	var identifier string
	switch engine {
	// The MySQL family, including StarRocks and Doris, quotes identifiers with
	// backticks. MariaDB shares the MySQL driver and its views keep a bare query
	// body, so it needs the same quoting as MySQL.
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB, storepb.Engine_DORIS, storepb.Engine_STARROCKS:
		identifier = "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case storepb.Engine_POSTGRES:
		identifier = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	case storepb.Engine_MSSQL:
		identifier = "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		// Engines this build has no analyzer for keep the object name as written:
		// they are skipped before the wrapper's identifier could matter, and the
		// fallback keeps that skip visible instead of turning it into a build error.
		identifier = name
	}

	return definition, fmt.Sprintf("%s %s AS %s", keyword, identifier, definition), nil
}

// isCompleteStatement reports whether a stored definition is already a whole
// CREATE statement rather than a bare query body. Leading whitespace and
// comments are skipped first: a complete DDL commonly opens with a `--` or
// `/* */` note, and wrapping such a statement in "CREATE VIEW ... AS" makes it
// unparseable. A bare query body can never begin with CREATE, so the check is
// safe for every engine.
func isCompleteStatement(definition string) bool {
	rest := strings.TrimSpace(definition)
	for {
		switch {
		case strings.HasPrefix(rest, "--"):
			lineEnd := strings.IndexByte(rest, '\n')
			if lineEnd < 0 {
				return false
			}
			rest = strings.TrimSpace(rest[lineEnd+1:])
		case strings.HasPrefix(rest, "/*"):
			commentEnd := strings.Index(rest[2:], "*/")
			if commentEnd < 0 {
				return false
			}
			rest = strings.TrimSpace(rest[2+commentEnd+2:])
		default:
			return hasCreateKeyword(rest)
		}
	}
}

// hasCreateKeyword reports whether s starts with the CREATE keyword followed by
// a separator. It accepts any whitespace between CREATE and what follows, so
// "CREATE\nVIEW" counts as a complete statement.
func hasCreateKeyword(s string) bool {
	const keyword = "CREATE"
	if len(s) < len(keyword) || !strings.EqualFold(s[:len(keyword)], keyword) {
		return false
	}
	rest := s[len(keyword):]
	return len(rest) > 0 && unicode.IsSpace(rune(rest[0]))
}
