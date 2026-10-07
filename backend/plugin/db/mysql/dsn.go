package mysql

import (
	"fmt"
	"net/url"
	"strings"
)

// BuildDSN renders the connection string for a MySQL-wire data source.
//
// databaseName is not a constant chosen at registration time: a schema sync
// reads it out of the target instance's own catalog. Interpolated as-is, a
// database named `app?tls=false&` would end the DSN's path segment and turn the
// rest into driver parameters — the connection would drop TLS, point at the
// database `app` and let whoever can create a database on the target rewrite the
// connection the platform opens with its privileged sync account. The name is
// therefore escaped with url.PathEscape, which is what the driver's own
// Config.FormatDSN applies to DBName, so an escaped name round-trips through
// ParseDSN unchanged.
//
// It is exported so StarRocks and Doris build their DSN the same way instead of
// re-deriving the template.
func BuildDSN(username, password, protocol, host, port, databaseName string, params []string) string {
	return fmt.Sprintf("%s:%s@%s(%s:%s)/%s?%s",
		username, password, protocol, host, port, url.PathEscape(databaseName), strings.Join(params, "&"))
}
