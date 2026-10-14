-- Deliberately awkward MSSQL objects. Each one is here to make the chain show a
-- boundary it would otherwise hide.
--
-- Nothing here is a defect kept for its own sake. An analyzer either models a
-- statement, models part of it and reports the rest, or fails it whole and says
-- why - and only the first two are visible as lineage. An object whose boundary
-- nobody wrote down is an object whose lineage can quietly be half a graph, so
-- every shape in this file is expected to leave a trace: a diagnostic on
-- column_lineage_version, or a failure message there, or (for a routine) no row at
-- all, because a routine body is not an object the analyzer reads.
--
-- Apply with: sqlcmd -S 127.0.0.1 -U sa -C -b -d e2e -i 60_gap_fixtures.sql
USE e2e;
GO

-- A PIVOT whose source is a derived table. The pivot carries its grouping column
-- (region) through, but only a source whose registered column list is known can be
-- enumerated, and a derived table has none. The view keeps the three aggregated
-- columns and reports `region` as an unresolved output target - the gap, not a
-- silently missing column.
CREATE OR ALTER VIEW e2e_ads.v_pivot_over_derived AS
SELECT region,
       [web],
       [store],
       [partner]
FROM (
    SELECT region, channel, net_line_amount
    FROM e2e_dwd.fact_daily_sales
) AS src
PIVOT (
    SUM(net_line_amount) FOR channel IN ([web], [store], [partner])
) AS p;
GO

-- An inline table-valued function and a view that reads it. A function body is a
-- routine, and a call is not a relation the registry can address: whatever the view
-- gets from it has no upstream object to point at.
CREATE OR ALTER FUNCTION e2e_dwd.fn_region_net (@region NVARCHAR(32))
RETURNS TABLE
AS
RETURN (
    SELECT region,
           SUM(net_line_amount) AS net_line_amount,
           SUM(order_count) AS order_count
    FROM e2e_dwd.fact_daily_sales
    WHERE region = @region
    GROUP BY region
);
GO

CREATE OR ALTER VIEW e2e_ads.v_tvf_source AS
SELECT f.region,
       f.net_line_amount,
       f.order_count,
       m.customer_count
FROM e2e_dwd.fn_region_net(N'EMEA') AS f
JOIN e2e_dwd.v_customer_mix AS m ON m.region = f.region;
GO

-- A table hint written after the alias is covered in 50_ads_views.sql, in the
-- spelling SQL Server accepts; the two orders the pinned parser cannot read
-- (`t WITH (NOLOCK) x` and `t AS x (NOLOCK)`) are not creatable on this server, so
-- they cannot appear as objects here.

-- A parenthesised view body: legal in T-SQL, and the shape the pinned parser is
-- documented not to read. The statement fails whole, so the runner records the
-- parse error on the version row and stores no lineage for this view - which is
-- the point: a definition nobody can read must not be indistinguishable from a
-- definition that has no lineage.
CREATE OR ALTER VIEW e2e_ads.v_parenthesised AS
(
    SELECT region,
           net_line_amount,
           order_count
    FROM e2e_dwd.fact_daily_sales
);
GO

-- A routine with a table variable and dynamic SQL. The static analyzer cannot see
-- through either and does not try: a table variable is not a registry object, and
-- the text handed to sp_executesql is not known until the statement runs.
CREATE OR ALTER PROCEDURE e2e_ads.usp_refresh_kpi
AS
BEGIN
    SET NOCOUNT ON;

    DECLARE @tv TABLE (region NVARCHAR(32), net_line_amount DECIMAL(16, 2));

    INSERT INTO @tv (region, net_line_amount)
    SELECT region, SUM(net_line_amount)
    FROM e2e_dwd.fact_daily_sales
    GROUP BY region;

    DECLARE @sql NVARCHAR(MAX) = N'UPDATE e2e_dwd.dim_customer SET refreshed_at = SYSUTCDATETIME() WHERE 1 = 0';
    EXEC sp_executesql @sql;
END;
GO
