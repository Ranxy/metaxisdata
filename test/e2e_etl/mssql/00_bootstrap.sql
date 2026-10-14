-- MSSQL side of the e2e chain: the e2e database and its three schemas.
--
-- Apply as sa against master (install.sh does this):
--   sqlcmd -S 127.0.0.1 -U sa -C -b -d master -i 00_bootstrap.sql
--
-- The database is separate from metaxis_demo, the hand-made instance fixture, so
-- that the chain's objects and its own are never confused in the registry.
IF DB_ID('e2e') IS NULL CREATE DATABASE e2e;
GO

USE e2e;
GO

IF SCHEMA_ID('e2e_ods') IS NULL EXEC('CREATE SCHEMA e2e_ods');
IF SCHEMA_ID('e2e_dwd') IS NULL EXEC('CREATE SCHEMA e2e_dwd');
IF SCHEMA_ID('e2e_ads') IS NULL EXEC('CREATE SCHEMA e2e_ads');
GO
