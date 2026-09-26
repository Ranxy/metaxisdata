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


def main() -> int:
    root = pathlib.Path(__file__).resolve().parent.parent
    mysql_to_pg_dir = root / "datax" / "mysql_to_pg"
    pg_to_sr_dir = root / "datax" / "pg_to_sr"
    mysql_to_pg_dir.mkdir(parents=True, exist_ok=True)
    pg_to_sr_dir.mkdir(parents=True, exist_ok=True)

    for table, columns in ODS_TABLES.items():
        path = mysql_to_pg_dir / f"{table}.json"
        path.write_text(json.dumps(mysql_to_pg(table, columns), indent=2) + "\n")
        print(path.relative_to(root))

    for source, target, columns in SR_TABLES:
        path = pg_to_sr_dir / f"{target}.json"
        path.write_text(json.dumps(pg_to_sr(source, target, columns), indent=2) + "\n")
        print(path.relative_to(root))

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
