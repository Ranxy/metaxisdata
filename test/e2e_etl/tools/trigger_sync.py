#!/usr/bin/env python3
"""Sync instances, so the platform sees the objects the chain just created.

Every hop creates objects after the last sync: views, tables and databases the
registry cannot know about until it syncs again. An instance carries its own sync
interval, and an instance with none is never synced at all -- the MSSQL instance this
fixture uses has no interval, so without this the database and the views the chain
creates stay invisible, no matter how many times the chain runs.

A sync call returns as soon as the platform has queued the instance, and the analysis
of what it found runs behind it, so follow it with a look at verify_chain.sql.

Usage:
    python3 trigger_sync.py --email admin@example.com --password … ms-sql-1
    python3 trigger_sync.py --email … --password …          # every instance

The credentials can also come from METAXISDATA_EMAIL / METAXISDATA_PASSWORD, and the
server from METAXISDATA_URL (default http://localhost:8083).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request


def connect_call(url: str, service: str, method: str, payload: dict, token: str = "") -> tuple[int, object]:
    request = urllib.request.Request(
        f"{url.rstrip('/')}/metaxisdata.v1.{service}/{method}",
        data=json.dumps(payload).encode(),
        method="POST",
    )
    request.add_header("Content-Type", "application/json")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(request, timeout=120) as response:
            body = response.read().decode()
            return response.status, json.loads(body or "{}")
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("instances", nargs="*", help="instance resource IDs; default: every instance")
    parser.add_argument("--url", default=os.environ.get("METAXISDATA_URL", "http://localhost:8083"))
    parser.add_argument("--email", default=os.environ.get("METAXISDATA_EMAIL", ""))
    parser.add_argument("--password", default=os.environ.get("METAXISDATA_PASSWORD", ""))
    args = parser.parse_args()

    if not args.email or not args.password:
        print("pass --email and --password (or set METAXISDATA_EMAIL / METAXISDATA_PASSWORD)", file=sys.stderr)
        return 2

    status, body = connect_call(args.url, "AuthService", "Login", {"email": args.email, "password": args.password})
    if status != 200 or not isinstance(body, dict) or not body.get("token"):
        print(f"sign-in failed: {status} {body}", file=sys.stderr)
        return 1
    token = body["token"]

    names = list(args.instances)
    if not names:
        status, body = connect_call(args.url, "InstanceService", "ListInstances", {}, token)
        if status != 200:
            print(f"listing instances failed: {status} {body}", file=sys.stderr)
            return 1
        names = [instance["name"].removeprefix("instances/") for instance in body.get("instances", [])]

    # enable_full_sync discovers databases the registry has never seen; without it a
    # sync only refreshes the databases it already has.
    status, body = connect_call(
        args.url,
        "InstanceService",
        "BatchSyncInstances",
        {"requests": [{"name": f"instances/{name}", "enableFullSync": True} for name in names]},
        token,
    )
    if status != 200:
        print(f"sync failed: {status} {body}", file=sys.stderr)
        return 1

    for result in body.get("results", []):
        if result.get("error"):
            print(f"  {result.get('name')}: {result['error']}")
            continue
        # `databases` is the databases the full sync discovered, not everything the
        # instance holds: an empty list is the normal answer once they are registered.
        discovered = result.get("databases", [])
        found = f"discovered {', '.join(discovered)}" if discovered else "no new database"
        print(f"  {result.get('name')}: {found}")
    print("synced; the analysis of what it found runs behind it")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
