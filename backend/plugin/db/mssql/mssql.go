// Package mssql is the plugin for MSSQL driver.
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	// Import the MSSQL driver.
	_ "github.com/microsoft/go-mssqldb"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

var (
	_ db.Driver = (*Driver)(nil)
)

func init() {
	db.Register(storepb.Engine_MSSQL, newDriver)
}

// Driver is the MSSQL driver.
type Driver struct {
	db           *sql.DB
	databaseName string

	// certFilePath is the temporary file holding the server CA. It is removed
	// on Close because the driver only accepts a certificate file path.
	certFilePath string
}

func newDriver() db.Driver {
	return &Driver{}
}

// Open opens a MSSQL driver.
func (d *Driver) Open(_ context.Context, _ storepb.Engine, config db.ConnectionConfig) (db.Driver, error) {
	query := url.Values{}
	query.Add("app name", "metaxisdata")

	databaseName := config.ConnectionContext.DatabaseName
	if databaseName == "" {
		databaseName = config.DataSource.GetDatabase()
	}
	if databaseName != "" {
		query.Add("database", databaseName)
	}

	// In order to be compatible with db servers that only support old versions
	// of TLS.
	// https://github.com/microsoft/go-mssqldb/issues/33
	query.Add("tlsmin", "1.0")

	for key, value := range config.DataSource.GetExtraConnectionParameters() {
		query.Add(key, value)
	}

	if config.DataSource.GetUseSsl() {
		query.Set("encrypt", "true")
		if config.DataSource.GetVerifyTlsCertificate() {
			query.Set("TrustServerCertificate", "false")
			if config.DataSource.GetSslCa() != "" {
				certFilePath, err := d.writeCertificate(config.DataSource.GetSslCa())
				if err != nil {
					return nil, err
				}
				query.Set("certificate", certFilePath)
			}
		} else {
			query.Set("TrustServerCertificate", "true")
		}
	}

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(config.DataSource.Username, config.Password),
		Host:     fmt.Sprintf("%s:%s", config.DataSource.Host, config.DataSource.Port),
		RawQuery: query.Encode(),
	}
	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		return nil, err
	}
	d.db = db
	d.databaseName = config.ConnectionContext.DatabaseName
	return d, nil
}

// writeCertificate writes the PEM encoded server CA to a temporary file, which
// the driver reads instead of accepting the certificate content directly.
func (d *Driver) writeCertificate(ca string) (string, error) {
	const pattern = "cert-*.pem"
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", errors.Wrapf(err, "failed to create a temporary file with pattern %s", pattern)
	}
	fName := file.Name()
	if _, err := file.WriteString(ca); err != nil {
		_ = file.Close()
		_ = os.Remove(fName)
		return "", errors.Wrapf(err, "failed to write the certificate to file %s", fName)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(fName)
		return "", errors.Wrapf(err, "failed to close file %s", fName)
	}
	d.certFilePath = fName
	return fName, nil
}

// Close closes the driver.
func (d *Driver) Close(_ context.Context) error {
	if d.certFilePath != "" {
		if err := os.Remove(d.certFilePath); err != nil {
			slog.Warn("failed to delete temporary file", slog.String("path", d.certFilePath), log.WithError(err))
		}
	}
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// Ping pings the database.
func (d *Driver) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// GetDB gets the database.
func (d *Driver) GetDB() *sql.DB {
	return d.db
}
