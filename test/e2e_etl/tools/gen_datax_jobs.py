#!/usr/bin/env python3
"""Generate the DataX job configs for both hops of the e2e lineage chain.

Every column is listed explicitly (never "*") because the DataX OpenLineage
provider builds its schema facet from that list; that facet is what lets the
ingester infer column-level lineage for a table-to-table copy.

Usage:
    python3 gen_datax_jobs.py
"""

from __future__ import annotations

import json
import pathlib

MYSQL_HOST, MYSQL_PORT, MYSQL_DB = "127.0.0.1", "4417", "e2e_ods"
PG_HOST, PG_PORT, PG_DB = "localhost", "5432", "e2e"
SR_HOST, SR_PORT, SR_DB = "127.0.0.1", "9030", "e2e_ods"
# FE HTTP entry (host 8030 -> container nginx feproxy 8080); Stream Load must not
# use the MySQL protocol port 9030.
SR_HTTP_PORT = "8030"

MYSQL_USER, MYSQL_PASSWORD = "dev", "dev"
PG_USER, PG_PASSWORD = "dev", "dev"
# StarRocks root has no password in the local all-in-one container.
SR_USER, SR_PASSWORD = "root", ""

# The SQL Server (MSSQL) side of the chain. The password is deliberately not in
# this file: it is a real credential of the local container, and this file is
# committed, so the job configs carry an empty password and install.sh fills it
# in from MSSQL_SA_PASSWORD when it deploys them.
MSSQL_HOST, MSSQL_PORT, MSSQL_DB = "127.0.0.1", "1433", "e2e"
MSSQL_USER, MSSQL_PASSWORD = "sa", ""
# The sqljdbc driver is addressed with the pre-4.2 spelling; the database is part
# of the URL because a DataX SQL Server connection has no selectedDatabase field.
MSSQL_JDBC_URL = f"jdbc:sqlserver://{MSSQL_HOST}:{MSSQL_PORT};databaseName={MSSQL_DB}"

# The Doris side of the chain. The FE's MySQL-protocol port is 9031 on the host
# (the StarRocks container already owns 9030), and its HTTP port is 8031: Stream
# Load is an HTTP API, not a MySQL statement, so the writer needs both. The local
# deployment has one FE and one BE, and root has no password.
DORIS_HOST, DORIS_PORT, DORIS_DB = "127.0.0.1", "9031", "e2e_ods"
DORIS_HTTP_PORT = "8031"
DORIS_USER, DORIS_PASSWORD = "root", ""
# The rdbms readers DataX ships reject an empty password, so the copy out of Doris
# reads as the fixture's read-only user (created by doris/20_ads_ddl.sql) instead.
DORIS_READER_USER, DORIS_READER_PASSWORD = "e2e_reader", "e2e_reader"
# The reader's URL names the database and the table stays unqualified, because the
# provider's MySQL plugin prefixes the URL's database to whatever `table` says. A
# qualified table whose database differs from the URL's therefore becomes a
# three-part dataset name ("e2e_ods.e2e_ads.v_doris_kpi_daily"), and the resolver
# reads three parts as database.schema.table: Doris has no schema, so that name
# resolves to a GUID no synced object has. FINDINGS F10 records the run that found
# it; verify_chain.sql sections 25 and 31 are the gates for the corrected name.
DORIS_READER_JDBC_URL = (
    f"jdbc:mysql://{DORIS_HOST}:{DORIS_PORT}/e2e_ads"
    "?useSSL=false&allowPublicKeyRetrieval=true&serverTimezone=UTC"
)
DORIS_JDBC_URL = (
    f"jdbc:mysql://{DORIS_HOST}:{DORIS_PORT}/{DORIS_DB}"
    "?useSSL=false&allowPublicKeyRetrieval=true&serverTimezone=UTC"
)

ODS_TABLES: dict[str, list[str]] = {
    "regions": ["country", "country_name", "region", "sub_region", "is_emerging"],
    "sales_reps": ["rep_id", "rep_name", "team", "manager_id", "hired_on"],
    "customers": [
        "customer_id", "customer_code", "full_name", "email", "phone", "city",
        "country", "signup_date", "status", "loyalty_tier", "credit_limit",
        "created_at", "updated_at",
    ],
    "products": [
        "product_id", "sku", "product_name", "category", "sub_category", "brand",
        "unit_price", "unit_cost", "weight_kg", "is_active", "launched_on",
    ],
    "orders": [
        "order_id", "order_no", "customer_id", "sales_rep_id", "order_ts", "order_date",
        "status", "channel", "currency", "item_count", "gross_amount", "discount_amount",
        "shipping_fee", "tax_amount", "net_amount", "coupon_code",
    ],
    "order_items": [
        "item_id", "order_id", "product_id", "quantity", "unit_price", "discount_pct",
        "line_amount", "warehouse_code",
    ],
    "payments": [
        "payment_id", "order_id", "payment_ts", "method", "amount", "fee", "currency",
        "status", "gateway_ref",
    ],
    "refunds": ["refund_id", "payment_id", "order_id", "refund_ts", "amount", "reason_code", "status"],
}

# (source schema-qualified relation, target StarRocks table, columns)
SR_TABLES: list[tuple[str, str, list[str]]] = [
    (
        "e2e_dwd.dwd_order_fact",
        "dwd_order_fact",
        [
            "order_id", "order_no", "customer_id", "customer_code", "customer_name",
            "country", "region", "sub_region", "sales_rep_id", "rep_name", "rep_team",
            "order_date", "order_ts", "status", "status_group", "channel", "currency",
            "item_count", "gross_amount", "discount_amount", "shipping_fee", "tax_amount",
            "net_amount", "line_count", "units", "line_revenue", "top_category",
            "paid_amount", "refund_amount", "balance_due", "payment_coverage",
            "customer_segment", "month_net_rank", "loaded_at",
        ],
    ),
    (
        "e2e_dwd.dwd_customer_360",
        "dwd_customer_360",
        [
            "customer_id", "customer_code", "customer_name", "country", "region",
            "sub_region", "city", "status", "loyalty_tier", "tenure_days", "order_count",
            "lifetime_gross", "lifetime_net", "avg_order_value", "lifetime_units",
            "paid_amount", "refund_amount", "net_collected", "balance_due",
            "collection_rate", "customer_segment", "value_rank", "loaded_at",
        ],
    ),
    (
        "e2e_dwd.mv_daily_sales",
        "mv_daily_sales",
        [
            "sales_date", "channel", "region", "currency", "order_count",
            "customer_count", "units", "gross_amount", "discount_amount",
            "net_line_amount", "avg_line_amount",
        ],
    ),
    (
        "e2e_ads.ads_daily_sales",
        "ads_daily_sales",
        [
            "sales_date", "channel", "region", "currency", "order_count",
            "customer_count", "units", "gross_amount", "discount_amount",
            "net_line_amount", "avg_line_amount", "order_share_pct", "revenue_band",
            "loaded_at",
        ],
    ),
    (
        "e2e_ads.ads_customer_segment",
        "ads_customer_segment",
        [
            "customer_segment", "country", "region", "customer_count", "total_net",
            "avg_net", "avg_orders", "vip_count", "segment_share_pct",
            "top_customer_id", "top_customer_name", "loaded_at",
        ],
    ),
]

_SR_COLUMNS: dict[str, list[str]] = {target: columns for _, target, columns in SR_TABLES}

# (source schema-qualified relation, MSSQL landing table, columns)
#
# The columns are the ones the StarRocks hop copies, on purpose: both hops read
# the same PostgreSQL serving layer, so the PG side has one place where a column
# list is written down.
PG_TO_MSSQL: list[tuple[str, str, list[str]]] = [
    ("e2e_dwd.dwd_customer_360", "e2e_ods.dwd_customer_360", _SR_COLUMNS["dwd_customer_360"]),
    ("e2e_ads.ads_daily_sales", "e2e_ods.ads_daily_sales", _SR_COLUMNS["ads_daily_sales"]),
]

# (MSSQL view to read, StarRocks landing table, columns)
#
# The source is a view rather than a table: the edge into StarRocks then starts at
# an object the MSSQL analyzer itself produced, so the two producers of this chain
# meet at the same node.
MSSQL_TO_SR: list[tuple[str, str, list[str]]] = [
    (
        "e2e_ads.v_kpi_daily",
        "mssql_kpi_daily",
        [
            "region", "selling_days", "order_count", "net_line_amount", "customer_net",
            "revenue_rank", "customer_count", "high_value_count", "balance_due",
            "orders_per_customer", "revenue_band",
        ],
    ),
]

# (source schema-qualified relation, Doris landing table, columns)
#
# The same two PostgreSQL serving-layer tables the StarRocks and MSSQL hops copy,
# so all three read one column list from _SR_COLUMNS.
PG_TO_DORIS: list[tuple[str, str, list[str]]] = [
    ("e2e_dwd.dwd_customer_360", "dwd_customer_360", _SR_COLUMNS["dwd_customer_360"]),
    ("e2e_ads.ads_daily_sales", "ads_daily_sales", _SR_COLUMNS["ads_daily_sales"]),
]

# (Doris view to read, StarRocks landing table, columns)
#
# The source is a Doris view the analyzer owns, and the reader is the provider's
# mysqlreader: Doris speaks the MySQL protocol, and the resolver matches a dataset
# by host and port rather than by URL scheme, so the Doris instance answers for a
# mysql:// namespace on its own port. The view is named without its database because
# the reader URL carries it - see DORIS_READER_JDBC_URL.
DORIS_TO_SR: list[tuple[str, str, list[str]]] = [
    (
        "v_doris_kpi_daily",
        "doris_kpi_daily",
        [
            "region", "order_days", "order_count", "net_line_amount",
            "customer_count", "region_pool", "top_rank", "revenue_band",
        ],
    ),
]

SETTING = {
    "speed": {"channel": 1},
    "errorLimit": {"record": 0, "percentage": 0.02},
}


def mysql_to_pg(table: str, columns: list[str]) -> dict:
    return {
        "job": {
            "setting": SETTING,
            "content": [
                {
                    "reader": {
                        "name": "mysqlreader",
                        "parameter": {
                            "username": MYSQL_USER,
                            "password": MYSQL_PASSWORD,
                            "column": columns,
                            "connection": [
                                {
                                    "jdbcUrl": [
                                        f"jdbc:mysql://{MYSQL_HOST}:{MYSQL_PORT}/{MYSQL_DB}"
                                        "?useSSL=false&allowPublicKeyRetrieval=true&serverTimezone=UTC"
                                    ],
                                    "table": [table],
                                }
                            ],
                        },
                    },
                    "writer": {
                        "name": "postgresqlwriter",
                        "parameter": {
                            "username": PG_USER,
                            "password": PG_PASSWORD,
                            "column": columns,
                            "preSql": [f"delete from e2e_ods.{table}"],
                            "connection": [
                                {
                                    "jdbcUrl": f"jdbc:postgresql://{PG_HOST}:{PG_PORT}/{PG_DB}",
                                    "table": [f"e2e_ods.{table}"],
                                }
                            ],
                        },
                    },
                }
            ],
        }
    }


def pg_to_sr(source: str, target: str, columns: list[str]) -> dict:
    return {
        "job": {
            "setting": SETTING,
            "content": [
                {
                    "reader": {
                        "name": "postgresqlreader",
                        "parameter": {
                            "username": PG_USER,
                            "password": PG_PASSWORD,
                            "column": columns,
                            "connection": [
                                {
                                    "jdbcUrl": [f"jdbc:postgresql://{PG_HOST}:{PG_PORT}/{PG_DB}"],
                                    "table": [source],
                                }
                            ],
                        },
                    },
                    "writer": {
                        "name": "starrockswriter",
                        "parameter": {
                            "username": SR_USER,
                            "password": SR_PASSWORD,
                            "column": columns,
                            # StarRocks refuses a bare DELETE (it requires a WHERE clause).
                            "preSql": [f"truncate table {SR_DB}.{target}"],
                            "loadUrl": [f"{SR_HOST}:{SR_HTTP_PORT}"],
                            "connection": [
                                {
                                    "jdbcUrl": f"jdbc:mysql://{SR_HOST}:{SR_PORT}/{SR_DB}",
                                    "selectedDatabase": SR_DB,
                                    "table": [target],
                                }
                            ],
                        },
                    },
                }
            ],
        }
    }


def pg_to_mssql(source: str, target: str, columns: list[str]) -> dict:
    return {
        "job": {
            "setting": SETTING,
            "content": [
                {
                    "reader": {
                        "name": "postgresqlreader",
                        "parameter": {
                            "username": PG_USER,
                            "password": PG_PASSWORD,
                            "column": columns,
                            "connection": [
                                {
                                    "jdbcUrl": [f"jdbc:postgresql://{PG_HOST}:{PG_PORT}/{PG_DB}"],
                                    "table": [source],
                                }
                            ],
                        },
                    },
                    "writer": {
                        "name": "sqlserverwriter",
                        "parameter": {
                            "username": MSSQL_USER,
                            "password": MSSQL_PASSWORD,
                            "column": columns,
                            "preSql": [f"delete from {target}"],
                            # No writeMode: the SQL Server writer accepts only an
                            # insert and rejects the field rather than defaulting it.
                            "connection": [
                                {
                                    "jdbcUrl": MSSQL_JDBC_URL,
                                    "table": [target],
                                }
                            ],
                        },
                    },
                }
            ],
        }
    }


def mssql_to_sr(source: str, target: str, columns: list[str]) -> dict:
    return {
        "job": {
            "setting": SETTING,
            "content": [
                {
                    "reader": {
                        "name": "sqlserverreader",
                        "parameter": {
                            "username": MSSQL_USER,
                            "password": MSSQL_PASSWORD,
                            "column": columns,
                            "connection": [
                                {
                                    "jdbcUrl": [MSSQL_JDBC_URL],
                                    "table": [source],
                                }
                            ],
                        },
                    },
                    "writer": {
                        "name": "starrockswriter",
                        "parameter": {
                            "username": SR_USER,
                            "password": SR_PASSWORD,
                            "column": columns,
                            "preSql": [f"truncate table {SR_DB}.{target}"],
                            "loadUrl": [f"{SR_HOST}:{SR_HTTP_PORT}"],
                            "connection": [
                                {
                                    "jdbcUrl": f"jdbc:mysql://{SR_HOST}:{SR_PORT}/{SR_DB}",
                                    "selectedDatabase": SR_DB,
                                    "table": [target],
                                }
                            ],
                        },
                    },
                }
            ],
        }
    }


def pg_to_doris(source: str, target: str, columns: list[str]) -> dict:
    return {
        "job": {
            "setting": SETTING,
            "content": [
                {
                    "reader": {
                        "name": "postgresqlreader",
                        "parameter": {
                            "username": PG_USER,
                            "password": PG_PASSWORD,
                            "column": columns,
                            "connection": [
                                {
                                    "jdbcUrl": [f"jdbc:postgresql://{PG_HOST}:{PG_PORT}/{PG_DB}"],
                                    "table": [source],
                                }
                            ],
                        },
                    },
                    "writer": {
                        "name": "doriswriter",
                        "parameter": {
                            "username": DORIS_USER,
                            "password": DORIS_PASSWORD,
                            "column": columns,
                            "preSql": [f"truncate table {DORIS_DB}.{target}"],
                            # FE HTTP entry: Stream Load is an HTTP API and must not
                            # use the MySQL-protocol port 9031. The FE answers with a
                            # redirect to its BE, whose own HTTP port is reachable
                            # from the host.
                            "loadUrl": [f"{DORIS_HOST}:{DORIS_HTTP_PORT}"],
                            "connection": [
                                {
                                    "jdbcUrl": DORIS_JDBC_URL,
                                    "selectedDatabase": DORIS_DB,
                                    "table": [target],
                                }
                            ],
                        },
                    },
                }
            ],
        }
    }


def doris_to_sr(source: str, target: str, columns: list[str]) -> dict:
    return {
        "job": {
            "setting": SETTING,
            "content": [
                {
                    "reader": {
                        # Doris speaks the MySQL protocol; the provider's mysqlreader
                        # reads the view, and its dataset resolves to the Doris
                        # instance because the resolver matches host and port.
                        "name": "mysqlreader",
                        "parameter": {
                            "username": DORIS_READER_USER,
                            "password": DORIS_READER_PASSWORD,
                            "column": columns,
                            "connection": [
                                {
                                    "jdbcUrl": [DORIS_READER_JDBC_URL],
                                    "table": [source],
                                }
                            ],
                        },
                    },
                    "writer": {
                        "name": "starrockswriter",
                        "parameter": {
                            "username": SR_USER,
                            "password": SR_PASSWORD,
                            "column": columns,
                            "preSql": [f"truncate table {SR_DB}.{target}"],
                            "loadUrl": [f"{SR_HOST}:{SR_HTTP_PORT}"],
                            "connection": [
                                {
                                    "jdbcUrl": f"jdbc:mysql://{SR_HOST}:{SR_PORT}/{SR_DB}",
                                    "selectedDatabase": SR_DB,
                                    "table": [target],
                                }
                            ],
                        },
                    },
                }
            ],
        }
    }


def main() -> int:
    root = pathlib.Path(__file__).resolve().parent.parent
    mysql_to_pg_dir = root / "datax" / "mysql_to_pg"
    pg_to_sr_dir = root / "datax" / "pg_to_sr"
    pg_to_mssql_dir = root / "datax" / "pg_to_mssql"
    mssql_to_sr_dir = root / "datax" / "mssql_to_sr"
    pg_to_doris_dir = root / "datax" / "pg_to_doris"
    doris_to_sr_dir = root / "datax" / "doris_to_sr"
    for directory in (mysql_to_pg_dir, pg_to_sr_dir, pg_to_mssql_dir, mssql_to_sr_dir,
                      pg_to_doris_dir, doris_to_sr_dir):
        directory.mkdir(parents=True, exist_ok=True)

    for table, columns in ODS_TABLES.items():
        path = mysql_to_pg_dir / f"{table}.json"
        path.write_text(json.dumps(mysql_to_pg(table, columns), indent=2) + "\n")
        print(path.relative_to(root))

    for source, target, columns in SR_TABLES:
        path = pg_to_sr_dir / f"{target}.json"
        path.write_text(json.dumps(pg_to_sr(source, target, columns), indent=2) + "\n")
        print(path.relative_to(root))

    for source, target, columns in PG_TO_MSSQL:
        # Named by the table alone: the DAGs derive a task id from the file name,
        # and a task id cannot carry the schema's dot.
        path = pg_to_mssql_dir / f"{target.rsplit('.', 1)[-1]}.json"
        path.write_text(json.dumps(pg_to_mssql(source, target, columns), indent=2) + "\n")
        print(path.relative_to(root))

    for source, target, columns in MSSQL_TO_SR:
        path = mssql_to_sr_dir / f"{target}.json"
        path.write_text(json.dumps(mssql_to_sr(source, target, columns), indent=2) + "\n")
        print(path.relative_to(root))

    for source, target, columns in PG_TO_DORIS:
        path = pg_to_doris_dir / f"{target}.json"
        path.write_text(json.dumps(pg_to_doris(source, target, columns), indent=2) + "\n")
        print(path.relative_to(root))

    for source, target, columns in DORIS_TO_SR:
        path = doris_to_sr_dir / f"{target}.json"
        path.write_text(json.dumps(doris_to_sr(source, target, columns), indent=2) + "\n")
        print(path.relative_to(root))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
