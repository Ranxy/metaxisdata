package starrocks

import (
	"context"
	"database/sql"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	dbmysql "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
)

// errUnknownTable is MySQL's "Unknown table" error, raised when an object is
// dropped between the information_schema listing and SHOW CREATE.
const errUnknownTable = 1051

var _ db.ObjectDefinitionReader = (*Driver)(nil)

// GetObjectDefinition returns the server's own DDL for an object. The catalog
// metadata cannot express StarRocks/Doris distribution, properties or
// partitioning, so the definition has to come from the server rather than being
// reconstructed.
//
// schema is unused deliberately: StarRocks and Doris have exactly one schema per
// database and the driver is connected to it, so d.databaseName already names the
// container. The parameter exists for engines with several schemas per database.
func (d *Driver) GetObjectDefinition(ctx context.Context, objectType storepb.MetaType, _ string, name string) (string, bool, error) {
	query, ok := showCreateQuery(d.databaseName, objectType, name)
	if !ok {
		return "", false, nil
	}

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == errUnknownTable {
			// The object vanished after it was listed; report it as having no
			// definition so the caller drops any stored one.
			return "", false, nil
		}
		return "", false, errors.Wrapf(err, "failed to execute %q", query)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", false, err
	}
	definitionIndex := createColumnIndex(columns)
	if definitionIndex < 0 {
		return "", false, errors.Errorf("query %q returned no create statement column", query)
	}

	var definition sql.NullString
	for rows.Next() {
		if err := scanCreateStatement(rows, len(columns), definitionIndex, &definition); err != nil {
			return "", false, err
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, errors.Wrapf(err, "failed to read %q result", query)
	}
	if !definition.Valid || definition.String == "" {
		return "", false, nil
	}
	// SHOW CREATE comes from the same catalog as the schema queries, and the DDL
	// is stored in a PostgreSQL TEXT column, which rejects U+0000 just like jsonb.
	return sanitizeCatalogString(definition.String), true, nil
}

// showCreateQuery builds the SHOW CREATE statement for an object type, reporting
// false for types this engine has no definition for.
func showCreateQuery(databaseName string, objectType storepb.MetaType, name string) (string, bool) {
	qualified := dbmysql.QuoteIdentifier(databaseName) + "." + dbmysql.QuoteIdentifier(name)
	switch objectType {
	case storepb.MetaType_TABLE:
		return "SHOW CREATE TABLE " + qualified, true
	case storepb.MetaType_VIEW:
		return "SHOW CREATE VIEW " + qualified, true
	case storepb.MetaType_MATERIALIZED_VIEW:
		return "SHOW CREATE MATERIALIZED VIEW " + qualified, true
	default:
		return "", false
	}
}

// createColumnIndex locates the column holding the DDL. SHOW CREATE returns a
// different arity per engine and version (a view is four columns on
// MySQL/StarRocks and two on Doris), so the column is found by name instead of
// by position.
func createColumnIndex(columns []string) int {
	for i, column := range columns {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(column)), "create ") {
			return i
		}
	}
	return -1
}

func scanCreateStatement(rows *sql.Rows, columnCount, definitionIndex int, definition *sql.NullString) error {
	dests := make([]any, columnCount)
	for i := range dests {
		if i == definitionIndex {
			dests[i] = definition
			continue
		}
		dests[i] = new(any)
	}
	return rows.Scan(dests...)
}
