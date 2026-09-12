package env

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/testcontainers/testcontainers-go"
)

const (
	postgresImage = "postgres:16-alpine"
	mysqlImage    = "mysql:8.4"

	integrationPostgresHostEnv = "INTEGRATION_POSTGRES_HOST"
	integrationPostgresPortEnv = "INTEGRATION_POSTGRES_PORT"
	integrationPostgresDBEnv   = "INTEGRATION_POSTGRES_DB"
	integrationMySQLHostEnv    = "INTEGRATION_MYSQL_HOST"
	integrationMySQLPortEnv    = "INTEGRATION_MYSQL_PORT"
)

// TestEnv owns real DB containers and pre-wired runner dependencies.
// TestEnv owns the database containers started for one integration test.
type TestEnv struct {
	containers []testcontainers.Container
}

func getenvDefault(envName, fallback string) string {
	value, ok := os.LookupEnv(envName)
	if !ok || value == "" {
		return fallback
	}
	return value
}

func seedMySQLSchema(ctx context.Context, host, port string) error {
	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
CREATE DATABASE IF NOT EXISTS it_app;
USE it_app;
CREATE TABLE IF NOT EXISTS users (
  id INT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  age INT NOT NULL
);
CREATE TABLE IF NOT EXISTS orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount DECIMAL(10,2) NOT NULL
);
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.name AS user_name, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
INSERT INTO users (id, name, age) VALUES (1, 'alice', 31)
  ON DUPLICATE KEY UPDATE name = VALUES(name), age = VALUES(age);
INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 9.99)
  ON DUPLICATE KEY UPDATE user_id = VALUES(user_id), amount = VALUES(amount);
`); err != nil {
		return err
	}

	return nil
}

func resetMySQLSchema(ctx context.Context, host, port string) error {
	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
DROP DATABASE IF EXISTS it_drop_me;
CREATE DATABASE IF NOT EXISTS it_app;
USE it_app;
DROP VIEW IF EXISTS user_order_view;
DROP TABLE IF EXISTS manual_sql_summary;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;
CREATE TABLE users (
  id INT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  age INT NOT NULL
);
CREATE TABLE orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount DECIMAL(10,2) NOT NULL
);
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.name AS user_name, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
INSERT INTO users (id, name, age) VALUES (1, 'alice', 31);
INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 9.99);
`); err != nil {
		return err
	}

	return nil
}

func seedPostgresSchema(ctx context.Context, host, port string) error {
	adminDB, err := sql.Open("pgx", postgresDSN(host, port, "postgres"))
	if err != nil {
		return err
	}
	defer adminDB.Close()

	if err := ensurePostgresDatabase(ctx, adminDB, "it_app"); err != nil {
		return err
	}

	appDB, err := sql.Open("pgx", postgresDSN(host, port, "it_app"))
	if err != nil {
		return err
	}
	defer appDB.Close()

	if _, err := appDB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS public.users (
  id INT PRIMARY KEY,
  name TEXT NOT NULL,
  age INT NOT NULL
);
CREATE TABLE IF NOT EXISTS public.orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount NUMERIC(10,2) NOT NULL
);
CREATE OR REPLACE VIEW public.user_order_view AS
SELECT u.id AS user_id, u.name AS user_name
FROM public.users u;
INSERT INTO public.users (id, name, age) VALUES (1, 'alice', 31)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, age = EXCLUDED.age;
INSERT INTO public.orders (id, user_id, amount) VALUES (1, 1, 9.99)
ON CONFLICT (id) DO UPDATE SET user_id = EXCLUDED.user_id, amount = EXCLUDED.amount;
`); err != nil {
		return err
	}

	return nil
}

func resetPostgresSchema(ctx context.Context, host, port string) error {
	adminDB, err := sql.Open("pgx", postgresDSN(host, port, "postgres"))
	if err != nil {
		return err
	}
	defer adminDB.Close()

	if _, err := adminDB.ExecContext(ctx, `
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname IN ('it_app', 'it_drop_me') AND pid <> pg_backend_pid();
`); err != nil {
		return err
	}
	if _, err := adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS it_drop_me`); err != nil {
		return err
	}
	if err := ensurePostgresDatabase(ctx, adminDB, "it_app"); err != nil {
		return err
	}

	appDB, err := sql.Open("pgx", postgresDSN(host, port, "it_app"))
	if err != nil {
		return err
	}
	defer appDB.Close()

	if _, err := appDB.ExecContext(ctx, `
DROP VIEW IF EXISTS public.user_order_view;
DROP TABLE IF EXISTS public.manual_sql_summary;
DROP TABLE IF EXISTS public.orders;
DROP TABLE IF EXISTS public.users;
CREATE TABLE public.users (
  id INT PRIMARY KEY,
  name TEXT NOT NULL,
  age INT NOT NULL
);
CREATE TABLE public.orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount NUMERIC(10,2) NOT NULL
);
CREATE OR REPLACE VIEW public.user_order_view AS
SELECT u.id AS user_id, u.name AS user_name
FROM public.users u;
INSERT INTO public.users (id, name, age) VALUES (1, 'alice', 31);
INSERT INTO public.orders (id, user_id, amount) VALUES (1, 1, 9.99);
`); err != nil {
		return err
	}

	return nil
}

func ensurePostgresDatabase(ctx context.Context, db *sql.DB, databaseName string) error {
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", databaseName).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quotePostgresIdentifier(databaseName)))
	return err
}

func postgresDSN(host, port, database string) string {
	return fmt.Sprintf("postgres://postgres:postgres@%s:%s/%s?sslmode=disable", host, port, database)
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// CreateMySQLInstance stores an active MySQL instance with one ADMIN datasource.
// CreatePostgresInstance stores an active PostgreSQL instance with one ADMIN datasource.
