-- MySQL fixture schema for test/e2e_quality.
--
-- One database, one table per scenario group, so a failing rule can be judged
-- by looking at one table. The seed (20_seed.sql) is deterministic: every rule
-- in rules/mysql.json expects an exact verdict and exact row counts from it.
--
--   docker exec -i mysql8-db-1 mysql -uroot -p"$MYSQL_ROOT_PASSWORD" < 10_schema.sql

DROP DATABASE IF EXISTS e2e_dq;
CREATE DATABASE e2e_dq DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
USE e2e_dq;

-- Parent table of the referential-integrity rule.
CREATE TABLE dq_reference_customers (
  id INT NOT NULL PRIMARY KEY,
  name VARCHAR(64) NOT NULL
);

-- The main defect table: 10 rows, each defect is described in 20_seed.sql.
CREATE TABLE dq_orders (
  id INT NOT NULL PRIMARY KEY,
  customer_email VARCHAR(128) NULL,
  credit_code VARCHAR(18) NOT NULL,
  external_ref VARCHAR(32) NOT NULL,
  amount DECIMAL(10,2) NOT NULL,
  status VARCHAR(16) NOT NULL,
  product_code VARCHAR(16) NOT NULL,
  customer_id INT NOT NULL,
  quantity INT NOT NULL,
  shipped_quantity INT NOT NULL,
  updated_at DATETIME NOT NULL
);

-- The reconciliation reference side of cross_table (same database, one statement).
CREATE TABLE dq_orders_rollup (
  id INT NOT NULL PRIMARY KEY
);

-- Grouped row-count rule: three buckets, one of them over the allowed size.
CREATE TABLE dq_groups (
  id INT NOT NULL PRIMARY KEY,
  bucket VARCHAR(8) NOT NULL
);

-- Freshness rule: every row is stale.
CREATE TABLE dq_heartbeat (
  id INT NOT NULL PRIMARY KEY,
  updated_at DATETIME NOT NULL
);

-- Nothing to judge: an aggregate over an empty table is NULL, so the default
-- on_empty (NOT_EVALUATED) applies.
CREATE TABLE dq_empty (
  id INT NOT NULL PRIMARY KEY,
  email VARCHAR(128) NULL,
  amount DECIMAL(10,2) NULL
);

-- Segment rule: two runs, the second loses one segment.
CREATE TABLE dq_segment (
  id INT NOT NULL PRIMARY KEY,
  region VARCHAR(16) NOT NULL
);

-- Schema-drift rule: created with a two-column snapshot, then a column is added.
CREATE TABLE dq_drift_target (
  id INT NOT NULL PRIMARY KEY,
  name VARCHAR(32) NOT NULL
);

-- Orphan rule: the table is dropped after the rule exists.
CREATE TABLE dq_orphan_target (
  id INT NOT NULL PRIMARY KEY
);

-- View target for the save-time rejection gate (a view only allows table-level rules).
CREATE VIEW dq_orders_view AS
  SELECT id, amount FROM dq_orders;
