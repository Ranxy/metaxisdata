package mysql

import "strings"

// QuoteIdentifier backtick-quotes a MySQL-wire identifier. Identifiers cannot be
// bound as statement parameters, so an embedded backtick is escaped by doubling.
// It is exported so the other MySQL-wire drivers (StarRocks, Doris) quote
// identifiers the same way instead of letting the escaping drift.
func QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// qualifiedIdentifier renders a database-qualified object name for the SHOW
// CREATE statements the sync path builds. The names come from the target
// instance's catalog, so they must not be able to escape their quoting.
func qualifiedIdentifier(databaseName, name string) string {
	return QuoteIdentifier(databaseName) + "." + QuoteIdentifier(name)
}
