// Package mysql is the plugin for MySQL driver.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.uber.org/multierr"
	"golang.org/x/crypto/ssh"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/plugin/db/util"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

var (
	baseTableType = "BASE TABLE"
	viewTableType = "VIEW"

	_ db.Driver = (*Driver)(nil)
)

func init() {
	db.Register(storepb.Engine_MYSQL, newDriver)
	db.Register(storepb.Engine_MARIADB, newDriver)
	db.Register(storepb.Engine_OCEANBASE, newDriver)
}

// validateMySQLExtraConnectionParameters validates that no dangerous parameters are present.
func validateMySQLExtraConnectionParameters(params map[string]string) error {
	for key := range params {
		// Normalize key to lowercase for case-insensitive comparison
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "allowallfiles" {
			// Disables file allowlist for LOAD DATA LOCAL INFILE and allows all files (might be insecure)
			return errors.Errorf("connection parameter %q is not allowed for security reasons", key)
		}
	}
	return nil
}

// Driver is the MySQL driver.
type Driver struct {
	dbType       storepb.Engine
	db           *sql.DB
	databaseName string
	sshClient    *ssh.Client

	// Called upon driver.Open() finishes.
	openCleanUp []func()
}

func newDriver() db.Driver {
	return &Driver{}
}

// Open opens a MySQL driver.
func (d *Driver) Open(_ context.Context, dbType storepb.Engine, connCfg db.ConnectionConfig) (db.Driver, error) {
	defer func() {
		for _, f := range d.openCleanUp {
			f()
		}
	}()

	dsn, err := d.getMySQLConnection(connCfg)

	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	d.dbType = dbType
	d.db = db
	// TODO(d): remove the work-around once we have clean-up the migration connection hack.
	db.SetConnMaxLifetime(2 * time.Hour)
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(15)
	d.databaseName = connCfg.ConnectionContext.DatabaseName

	return d, nil
}

func (d *Driver) getMySQLConnection(connCfg db.ConnectionConfig) (string, error) {
	protocol := "tcp"
	if strings.HasPrefix(connCfg.DataSource.Host, "/") {
		protocol = "unix"
	}

	params := []string{"multiStatements=true", "maxAllowedPacket=0"}
	if err := validateMySQLExtraConnectionParameters(connCfg.DataSource.GetExtraConnectionParameters()); err != nil {
		return "", err
	}
	for key, value := range connCfg.DataSource.GetExtraConnectionParameters() {
		params = append(params, fmt.Sprintf("%s=%s", key, value))
	}
	if connCfg.DataSource.GetSshHost() != "" {
		sshClient, err := util.GetSSHClient(connCfg.DataSource)
		if err != nil {
			return "", err
		}
		d.sshClient = sshClient
		protocol = "mysql-tcp-" + uuid.NewString()[:8]
		// Now we register the dialer with the ssh connection as a parameter.
		mysql.RegisterDialContext(protocol, func(_ context.Context, addr string) (net.Conn, error) {
			return sshClient.Dial("tcp", addr)
		})
	}

	tlscfg, err := util.GetTLSConfig(connCfg.DataSource)
	if err != nil {
		return "", errors.Wrap(err, "sql: tls config error")
	}
	tlsKey := uuid.NewString()
	if tlscfg != nil {
		if err := mysql.RegisterTLSConfig(tlsKey, tlscfg); err != nil {
			return "", errors.Wrap(err, "sql: failed to register tls config")
		}
		// TLS config is only used during sql.Open, so should be safe to deregister afterwards.
		d.openCleanUp = append(d.openCleanUp, func() { mysql.DeregisterTLSConfig(tlsKey) })
		params = append(params, fmt.Sprintf("tls=%s", tlsKey))
	}
	return fmt.Sprintf("%s:%s@%s(%s:%s)/%s?%s", connCfg.DataSource.Username, connCfg.Password, protocol, connCfg.DataSource.Host, connCfg.DataSource.Port, connCfg.ConnectionContext.DatabaseName, strings.Join(params, "&")), nil
}

// Close closes the driver.
func (d *Driver) Close(context.Context) error {
	var err error
	err = multierr.Append(err, d.db.Close())
	if d.sshClient != nil {
		err = multierr.Append(err, d.sshClient.Close())
	}
	return err
}

// Ping pings the database.
func (d *Driver) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// GetDB gets the database.
func (d *Driver) GetDB() *sql.DB {
	return d.db
}

// getVersion gets the version.
func (d *Driver) getVersion(ctx context.Context) (string, string, error) {
	query := "SELECT VERSION()"
	var version string
	if err := d.db.QueryRowContext(ctx, query).Scan(&version); err != nil {
		if err == sql.ErrNoRows {
			return "", "", common.FormatDBErrorEmptyRowWithQuery(query)
		}
		return "", "", util.FormatErrorWithQuery(err, query)
	}

	return parseVersion(version)
}

func parseVersion(version string) (string, string, error) {
	if loc := regexp.MustCompile(`^\d+.\d+.\d+`).FindStringIndex(version); loc != nil {
		return version[loc[0]:loc[1]], version[loc[1]:], nil
	}
	return "", "", errors.Errorf("failed to parse version %q", version)
}

func (d *Driver) StopConnectionByID(id string) error {
	// We cannot use placeholder parameter because TiDB doesn't accept it.
	_, err := d.db.Exec(fmt.Sprintf("KILL QUERY %s", id))
	return err
}
