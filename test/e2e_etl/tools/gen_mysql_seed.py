#!/usr/bin/env python3
"""Generate the deterministic MySQL seed for the e2e lineage fixture.

Writes a single SQL file with batched INSERTs. Amounts are internally
consistent (order totals are derived from their items, payments/refunds from
order totals) so the PG/StarRocks transforms can be reconciled against the
source rows.

Usage:
    python3 gen_mysql_seed.py > ../mysql/20_seed.sql
"""

from __future__ import annotations

import random
import sys
from datetime import date, datetime, timedelta
from decimal import Decimal, ROUND_HALF_UP

SEED = 20260407
BATCH = 200

REGIONS = [
    ("US", "United States", "Americas", "North America", 0),
    ("CA", "Canada", "Americas", "North America", 0),
    ("BR", "Brazil", "Americas", "Latin America", 1),
    ("MX", "Mexico", "Americas", "Latin America", 1),
    ("DE", "Germany", "EMEA", "Western Europe", 0),
    ("FR", "France", "EMEA", "Western Europe", 0),
    ("GB", "United Kingdom", "EMEA", "Western Europe", 0),
    ("PL", "Poland", "EMEA", "Eastern Europe", 1),
    ("JP", "Japan", "APAC", "East Asia", 0),
    ("SG", "Singapore", "APAC", "Southeast Asia", 0),
    ("AU", "Australia", "APAC", "Oceania", 0),
    ("IN", "India", "APAC", "South Asia", 1),
]

CITIES = {
    "US": ["Austin", "Seattle", "Chicago", "Boston"],
    "CA": ["Toronto", "Vancouver"],
    "BR": ["Sao Paulo", "Curitiba"],
    "MX": ["Guadalajara", "Monterrey"],
    "DE": ["Berlin", "Munich"],
    "FR": ["Lyon", "Nantes"],
    "GB": ["London", "Manchester"],
    "PL": ["Krakow", "Warsaw"],
    "JP": ["Tokyo", "Osaka"],
    "SG": ["Singapore"],
    "AU": ["Sydney", "Melbourne"],
    "IN": ["Bengaluru", "Pune"],
}

TEAMS = ["enterprise", "midmarket", "smb"]
CATEGORIES = {
    "electronics": ["audio", "wearables", "accessories"],
    "home": ["kitchen", "furniture", "decor"],
    "outdoor": ["camping", "cycling", None],
    "grocery": ["coffee", "snacks", None],
}
BRANDS = ["acme", "northwind", "globex", "initech", "umbrella", "soylent"]
CHANNELS = ["web", "mobile_app", "partner", "retail", "sales_rep"]
ORDER_STATUS = ["completed", "shipped", "pending", "cancelled", "returned"]
PAY_METHODS = ["card", "paypal", "wire", "gift_card", "balance"]
CURRENCIES = ["USD", "USD", "USD", "EUR", "GBP", "JPY", "SGD"]
WAREHOUSES = ["WH-EAST", "WH-WEST", "WH-CENT", "WH-APAC"]
REFUND_REASONS = ["damaged", "not_as_described", "late_delivery", "customer_remorse"]


def money(value) -> Decimal:
    return Decimal(value).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)


def lit(value) -> str:
    if value is None:
        return "NULL"
    if isinstance(value, Decimal):
        return str(value)
    if isinstance(value, (int,)):
        return str(value)
    if isinstance(value, datetime):
        return "'" + value.strftime("%Y-%m-%d %H:%M:%S") + "'"
    if isinstance(value, date):
        return "'" + value.isoformat() + "'"
    return "'" + str(value).replace("\\", "\\\\").replace("'", "''") + "'"


def emit(out, table: str, columns: list[str], rows: list[tuple]) -> None:
    if not rows:
        return
    for i in range(0, len(rows), BATCH):
        chunk = rows[i : i + BATCH]
        out.write(f"INSERT INTO {table} ({', '.join(columns)}) VALUES\n")
        out.write(",\n".join("(" + ", ".join(lit(v) for v in row) + ")" for row in chunk))
        out.write(";\n")
    out.write("\n")


def main() -> int:
    rnd = random.Random(SEED)

    reps, managers = [], []
    for rep_id in range(1, 13):
        team = TEAMS[(rep_id - 1) % len(TEAMS)]
        manager_id = None if rep_id <= 3 else rnd.randint(1, 3)
        reps.append((rep_id, f"Rep {rep_id:02d}", team, manager_id, date(2020 + rep_id % 5, 1 + rep_id % 12, 1 + rep_id % 27)))
    managers = [r[0] for r in reps]

    customers = []
    for cid in range(1, 301):
        country = REGIONS[rnd.randrange(len(REGIONS))][0]
        city = rnd.choice(CITIES[country])
        signup = date(2024, 1, 1) + timedelta(days=rnd.randrange(0, 730))
        created = datetime.combine(signup, datetime.min.time()) + timedelta(seconds=rnd.randrange(0, 86400))
        updated = created + timedelta(days=rnd.randrange(0, 60)) if rnd.random() < 0.6 else None
        customers.append(
            (
                cid,
                f"C{cid:06d}",
                f"Customer {cid:03d}",
                f"customer{cid:03d}@example.com" if rnd.random() > 0.05 else None,
                f"+1-555-{rnd.randrange(1000, 9999)}" if rnd.random() > 0.1 else None,
                city,
                country,
                signup,
                rnd.choices(["active", "inactive", "churned"], weights=[80, 15, 5])[0],
                rnd.choices(["bronze", "silver", "gold", "platinum"], weights=[45, 30, 20, 5])[0],
                money(rnd.choice([0, 1000, 2500, 5000, 10000])),
                created,
                updated,
            )
        )

    products = []
    for pid in range(1, 101):
        category, subs = rnd.choice(list(CATEGORIES.items()))
        sub = rnd.choice(subs)
        price = money(rnd.uniform(9.9, 899.0))
        cost = money(price * Decimal(str(rnd.uniform(0.35, 0.8))))
        products.append(
            (
                pid,
                f"SKU-{category[:3].upper()}-{pid:04d}",
                f"{rnd.choice(BRANDS).title()} {category.title()} Item {pid:03d}",
                category,
                sub,
                rnd.choice(BRANDS),
                price,
                cost,
                money(rnd.uniform(0.1, 25.0)) if rnd.random() > 0.15 else None,
                1 if rnd.random() > 0.12 else 0,
                date(2023, 1, 1) + timedelta(days=rnd.randrange(0, 900)) if rnd.random() > 0.1 else None,
            )
        )

    start = datetime(2025, 1, 1, 0, 0, 0)
    items, orders, payments, refunds = [], [], [], []
    item_id = payment_id = refund_id = 0

    for oid in range(1, 2001):
        order_ts = start + timedelta(minutes=rnd.randrange(0, 455 * 24 * 60))
        order_date = order_ts.date()
        customer = customers[rnd.randrange(len(customers))]
        status = rnd.choices(ORDER_STATUS, weights=[55, 25, 8, 7, 5])[0]
        currency = rnd.choice(CURRENCIES)
        rep_id = rnd.choice(managers) if rnd.random() > 0.25 else None
        n_items = rnd.choices([1, 2, 3, 4, 5], weights=[35, 30, 20, 10, 5])[0]

        gross = discount = Decimal("0.00")
        item_count = 0
        for _ in range(n_items):
            product = products[rnd.randrange(len(products))]
            qty = rnd.choices([1, 2, 3, 5], weights=[60, 25, 10, 5])[0]
            pct = Decimal(str(rnd.choice([0, 0, 0, 5, 10, 15, 25])))
            unit_price = product[6]
            line_gross = money(unit_price * qty)
            line_discount = money(line_gross * pct / 100)
            line_amount = money(line_gross - line_discount)
            item_id += 1
            item_count += qty
            gross += line_gross
            discount += line_discount
            items.append(
                (
                    item_id,
                    oid,
                    product[0],
                    qty,
                    unit_price,
                    pct,
                    line_amount,
                    rnd.choice(WAREHOUSES),
                )
            )

        shipping = money(rnd.choice([0, 4.99, 9.99, 14.99]))
        tax = money((gross - discount) * Decimal("0.08"))
        net = money(gross - discount + shipping + tax)
        coupon = rnd.choice(["SAVE10", "WELCOME5", None, None, None])
        orders.append(
            (
                oid,
                f"ORD-{order_date.strftime('%Y%m')}-{oid:06d}",
                customer[0],
                rep_id,
                order_ts,
                order_date,
                status,
                rnd.choice(CHANNELS),
                currency,
                item_count,
                money(gross),
                money(discount),
                shipping,
                tax,
                net,
                coupon,
            )
        )

        if status in ("completed", "shipped", "returned"):
            paid = order_ts + timedelta(hours=rnd.randrange(1, 72))
            splits = rnd.choices([1, 1, 1, 2], weights=[70, 15, 10, 5])[0]
            remaining = net
            for s in range(splits):
                amount = remaining if s == splits - 1 else money(net / splits)
                remaining = money(remaining - amount)
                payment_id += 1
                payments.append(
                    (
                        payment_id,
                        oid,
                        paid,
                        rnd.choice(PAY_METHODS),
                        amount,
                        money(amount * Decimal("0.029")),
                        currency,
                        "captured",
                        f"GW-{rnd.randrange(10**9, 10**10)}" if rnd.random() > 0.2 else None,
                    )
                )
        elif status == "pending" and rnd.random() < 0.3:
            payment_id += 1
            payments.append(
                (
                    payment_id,
                    oid,
                    order_ts + timedelta(hours=2),
                    rnd.choice(PAY_METHODS),
                    net,
                    Decimal("0.00"),
                    currency,
                    "pending",
                    None,
                )
            )

        if status == "returned":
            order_payments = [p for p in payments if p[1] == oid]
            if order_payments:
                pay = rnd.choice(order_payments)
                refund_id += 1
                refund_amount = money(pay[4] * Decimal(str(rnd.choice([1.0, 1.0, 0.5]))))
                refunds.append(
                    (
                        refund_id,
                        pay[0],
                        oid,
                        order_ts + timedelta(days=rnd.randrange(3, 30)),
                        refund_amount,
                        rnd.choice(REFUND_REASONS),
                        rnd.choices(["approved", "pending"], weights=[85, 15])[0],
                    )
                )

    out = sys.stdout
    out.write("-- Generated by tools/gen_mysql_seed.py -- do not edit by hand.\n")
    out.write(f"-- seed={SEED} customers={len(customers)} products={len(products)} orders={len(orders)} ")
    out.write(f"items={len(items)} payments={len(payments)} refunds={len(refunds)}\n")
    out.write("USE e2e_ods;\n\nSET autocommit=0;\n\n")

    emit(out, "regions", ["country", "country_name", "region", "sub_region", "is_emerging"], REGIONS)
    emit(out, "sales_reps", ["rep_id", "rep_name", "team", "manager_id", "hired_on"], reps)
    emit(
        out,
        "customers",
        [
            "customer_id", "customer_code", "full_name", "email", "phone", "city", "country",
            "signup_date", "status", "loyalty_tier", "credit_limit", "created_at", "updated_at",
        ],
        customers,
    )
    emit(
        out,
        "products",
        [
            "product_id", "sku", "product_name", "category", "sub_category", "brand",
            "unit_price", "unit_cost", "weight_kg", "is_active", "launched_on",
        ],
        products,
    )
    emit(
        out,
        "orders",
        [
            "order_id", "order_no", "customer_id", "sales_rep_id", "order_ts", "order_date",
            "status", "channel", "currency", "item_count", "gross_amount", "discount_amount",
            "shipping_fee", "tax_amount", "net_amount", "coupon_code",
        ],
        orders,
    )
    emit(
        out,
        "order_items",
        ["item_id", "order_id", "product_id", "quantity", "unit_price", "discount_pct", "line_amount", "warehouse_code"],
        items,
    )
    emit(
        out,
        "payments",
        ["payment_id", "order_id", "payment_ts", "method", "amount", "fee", "currency", "status", "gateway_ref"],
        payments,
    )
    emit(
        out,
        "refunds",
        ["refund_id", "payment_id", "order_id", "refund_ts", "amount", "reason_code", "status"],
        refunds,
    )
    out.write("COMMIT;\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
