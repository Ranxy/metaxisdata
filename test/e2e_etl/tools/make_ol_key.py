#!/usr/bin/env python3
"""Mint an OpenLineage ingestion API key row for the local metaxisdata instance.

The key is never stored in plain text: the server keeps a bcrypt hash plus a
sha256 lookup digest, so this script reproduces both. Prints shell assignments
on stdout; everything else goes to stderr.

Usage:
    python3 make_ol_key.py --description "e2e etl" > /tmp/ol_key.env
"""

from __future__ import annotations

import argparse
import hashlib
import os
import secrets
import sys


def mask(key: str) -> str:
    prefix, suffix = 8, 9
    if len(key) <= prefix + suffix:
        return key[:1] + "*" * (len(key) - 1)
    return key[:prefix] + "*" * (len(key) - prefix - suffix) + key[-suffix:]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--description", default="e2e etl integration test")
    parser.add_argument("--created-by", default="e2e-setup")
    parser.add_argument("--scope-namespace", default="")
    args = parser.parse_args()

    try:
        import bcrypt
    except ImportError:
        print("bcrypt is required: pip install bcrypt", file=sys.stderr)
        return 2

    key = "ol_" + secrets.token_hex(32)
    key_hash = bcrypt.hashpw(key.encode(), bcrypt.gensalt()).decode()
    digest = hashlib.sha256(key.encode()).hexdigest()

    # Single-quote every value: a bcrypt hash contains "$2b$12$...", which a
    # shell would otherwise expand when the file is sourced.
    print(f"OL_KEY='{key}'")
    print(f"OL_MASKED='{mask(key)}'")
    print(f"OL_HASH='{key_hash}'")
    print(f"OL_DIGEST='{digest}'")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
