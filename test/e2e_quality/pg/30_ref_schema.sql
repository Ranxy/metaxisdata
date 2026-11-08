-- PostgreSQL reference database schema, run against database e2e_dq_ref.
--
--   psql -h localhost -U dev -d e2e_dq_ref -f 30_ref_schema.sql

DROP SCHEMA IF EXISTS e2e_dq CASCADE;
CREATE SCHEMA e2e_dq;

CREATE TABLE e2e_dq.dq_orders_rollup (
  id INT NOT NULL PRIMARY KEY
);
