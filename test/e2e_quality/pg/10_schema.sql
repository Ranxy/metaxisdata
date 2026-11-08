-- PostgreSQL fixture schema, run against database e2e_dq as the dev user.
--
--   psql -h localhost -U dev -d e2e_dq -f 10_schema.sql

DROP SCHEMA IF EXISTS e2e_dq CASCADE;
CREATE SCHEMA e2e_dq;

CREATE TABLE e2e_dq.dq_orders (
  id INT NOT NULL PRIMARY KEY,
  customer_email VARCHAR(128) NULL,
  amount NUMERIC(10,2) NOT NULL,
  status VARCHAR(16) NOT NULL,
  -- A jsonb column: the save-time capability table must reject a range rule on it.
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  updated_at TIMESTAMP NOT NULL
);

-- The reference side read over its own connection from another database.
CREATE TABLE e2e_dq.dq_orders_rollup (
  id INT NOT NULL PRIMARY KEY
);

-- Percent sampling needs a relation big enough that TABLESAMPLE SYSTEM always
-- returns rows: every row here violates the sampled range rule, so a non-empty
-- sample always FAILs and an empty sample is the only flake (5000 rows removes it).
CREATE TABLE e2e_dq.dq_big (
  id INT NOT NULL PRIMARY KEY,
  amount NUMERIC(10,2) NOT NULL
);
