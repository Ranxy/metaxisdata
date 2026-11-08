#!/usr/bin/env python3
"""Drive the data-quality end-to-end fixture against a running Metaxisdata server.

Two subcommands:

    bootstrap   reset the MySQL/PostgreSQL fixtures, make sure the instances
                exist and carry a READ_ONLY source, and full-sync them.
    run         create the rule suite, run it, assert the exact verdict matrix,
                the issue/notification lifecycle, the outbound webhook, the
                scheduled tick, the report and the read models.

Everything the assertions read goes through the public ConnectRPC API, the way
the SPA does; the only direct database access is the fixture data itself
(MySQL/PostgreSQL) and one metadata-DB UPDATE that makes a target due (no API
can force a due target, and the minimum schedule interval is 15 minutes).

The expected single-run verdicts live in rules/mysql.json and rules/postgres.json;
the multi-run sequences (row_count_change, segment_check, schema_drift) are
asserted by the scenario functions that drive them.

Exit code is 0 only when every check passed.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import threading
import time
from datetime import datetime, timezone
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# ---- environment -----------------------------------------------------------

URL = os.environ.get("METAXISDATA_URL", "http://localhost:8083")
EMAIL = os.environ.get("METAXISDATA_EMAIL", "")
PASSWORD = os.environ.get("METAXISDATA_PASSWORD", "")

MYSQL_INSTANCE = os.environ.get("DQ_MYSQL_INSTANCE", "mysql-dev-1")
PG_INSTANCE = os.environ.get("DQ_PG_INSTANCE", "test-pg-1")
# A second instance the fixture owns, used only for the "no READ_ONLY source"
# scenario: deleting the source is safe there because nothing else uses it.
NOREAD_INSTANCE = os.environ.get("DQ_NOREAD_INSTANCE", "dq-noread")

MYSQL_CONTAINER = os.environ.get("MYSQL_CONTAINER", "mysql8-db-1")
MYSQL_ROOT_PASSWORD = os.environ.get("MYSQL_ROOT_PASSWORD", "rootadmin")
MYSQL_HOST = os.environ.get("DQ_MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = os.environ.get("DQ_MYSQL_PORT", "4417")

PG_HOST = os.environ.get("PG_HOST", "localhost")
PG_PORT = os.environ.get("PG_PORT", "5432")
PG_USER = os.environ.get("PG_USER", "dev")
PG_PASSWORD = os.environ.get("PG_PASSWORD", "dev")
# Superuser access for CREATE DATABASE mirrors test/e2e_etl: it goes through the
# local container, the same way install.sh of the lineage fixture does.
PG_CONTAINER = os.environ.get("PG_CONTAINER", "pg16-db-1")

METADATA_DB = os.environ.get("DQ_METADATA_DB", "metaxisdata")

MYSQL_DB = "e2e_dq"
PG_DB = "e2e_dq"
PG_REF_DB = "e2e_dq_ref"

READER_USER = "e2e_dq_reader"
READER_PASSWORD = "e2e-dq-reader"
READER_SOURCE_ID = "e2e-dq-readonly"

WAIT_EXECUTION = float(os.environ.get("DQ_EXECUTION_TIMEOUT", "90"))
WAIT_REGISTRY = float(os.environ.get("DQ_REGISTRY_TIMEOUT", "240"))

MYSQL_TABLES = [
    "dq_reference_customers", "dq_orders", "dq_orders_rollup", "dq_groups",
    "dq_heartbeat", "dq_empty", "dq_segment", "dq_drift_target", "dq_orphan_target",
]
MYSQL_GUID = lambda table: f"{MYSQL_INSTANCE};{MYSQL_DB};;{table}"  # noqa: E731
MYSQL_VIEW_GUID = f"{MYSQL_INSTANCE};{MYSQL_DB};;dq_orders_view"
NOREAD_GUID = f"{NOREAD_INSTANCE};{MYSQL_DB};;dq_orders"
PG_GUID = lambda table: f"{PG_INSTANCE};{PG_DB};{PG_DB};{table}"  # noqa: E731
PG_REF_GUID = f"{PG_INSTANCE};{PG_REF_DB};{PG_DB};dq_orders_rollup"

# The fixture's own target type (MetaType.TABLE) and the exact sizes its seed
# has; S5 and S9 assert against these instead of magic numbers.
TARGET_TYPE_TABLE = 4
DQ_ORDERS_RULE_COUNT = 15
DQ_ORDERS_COLUMN_COUNT = 11
DRIFT_COLUMNS_BEFORE = 2
DRIFT_COLUMNS_AFTER = 3


# ---- tiny output helpers ---------------------------------------------------


class Reporter:
    def __init__(self) -> None:
        self.passed = 0
        self.failed = 0

    def section(self, title: str) -> None:
        print(f"\n=== {title}")

    def check(self, ok: bool, label: str, detail: str = "") -> bool:
        if ok:
            self.passed += 1
            print(f"  ok    {label}")
        else:
            self.failed += 1
            print(f"  FAIL  {label}{f'  ({detail})' if detail else ''}")
        return ok

    def equal(self, got, want, label: str) -> bool:
        return self.check(got == want, label, f"got {got!r}, want {want!r}")

    def summary(self) -> int:
        total = self.passed + self.failed
        print(f"\n{'=' * 70}")
        print(f"{total} checks, {self.passed} passed, {self.failed} failed")
        return 0 if self.failed == 0 else 1


# ---- ConnectRPC client -----------------------------------------------------


class ApiError(Exception):
    def __init__(self, code: str, message: str, status: int) -> None:
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message
        self.status = status


class Client:
    def __init__(self) -> None:
        self.token = ""
        self.workspace = ""

    def call(self, service: str, method: str, payload: dict):
        req = urllib.request.Request(
            f"{URL.rstrip('/')}/metaxisdata.v1.{service}/{method}",
            data=json.dumps(payload).encode(), method="POST")
        req.add_header("Content-Type", "application/json")
        if self.token:
            req.add_header("Authorization", f"Bearer {self.token}")
        try:
            with urllib.request.urlopen(req, timeout=180) as resp:
                return json.loads(resp.read().decode() or "{}")
        except urllib.error.HTTPError as exc:
            body = exc.read().decode()
            try:
                parsed = json.loads(body)
            except json.JSONDecodeError:
                parsed = {"code": "http", "message": body[:500]}
            raise ApiError(parsed.get("code", "http"), parsed.get("message", ""), exc.code)

    # ---- auth / bootstrap ----

    def login(self) -> None:
        body = self.call("AuthService", "Login", {"email": EMAIL, "password": PASSWORD})
        self.token = body["token"]
        # The workspace name is not part of the login response; every DQ call
        # accepts workspaces/- for "the current workspace", so keep that.
        self.workspace = "workspaces/-"

    def instances(self) -> list[dict]:
        return self.call("InstanceService", "ListInstances", {}).get("instances", [])

    def instance(self, resource_id: str) -> dict:
        for inst in self.instances():
            if inst.get("name") == f"instances/{resource_id}":
                return inst
        raise SystemExit(f"instance {resource_id} is not registered; see README prerequisites")

    def ensure_instance(self, resource_id: str, title: str, host: str, port: str,
                        username: str, password: str) -> None:
        """Create the fixture-owned instance once; leave an existing one alone."""
        if f"instances/{resource_id}" in {inst.get("name") for inst in self.instances()}:
            return
        self.call("InstanceService", "CreateInstance", {
            "instanceId": resource_id,
            "instance": {
                "title": title, "engine": "MYSQL", "activation": True, "syncInterval": "0s",
                "dataSources": [{"type": "ADMIN", "host": host, "port": port,
                                 "username": username, "password": password}],
            },
        })

    def ensure_read_only_source(self, resource_id: str, host: str, port: str,
                                username: str, password: str, source_id: str) -> None:
        inst = self.instance(resource_id)
        sources = inst.get("dataSources", [])
        existing = next((s for s in sources if s.get("type") == "READ_ONLY"
                         and s.get("name", "").endswith("/" + source_id)), None)
        if existing:
            if (existing.get("host"), existing.get("port"), existing.get("username")) == (host, port, username):
                return
            self.call("InstanceService", "UpdateDataSource", {
                "dataSource": {
                    "name": f"instances/{resource_id}/dataSources/{source_id}",
                    "type": "READ_ONLY", "host": host, "port": port,
                    "username": username, "password": password,
                },
                "updateMask": "host,port,username,password",
            })
            return
        self.call("InstanceService", "CreateDataSource", {
            "parent": f"instances/{resource_id}",
            "dataSource": {
                "type": "READ_ONLY", "host": host, "port": port,
                "username": username, "password": password,
            },
            "dataSourceId": source_id,
        })

    def delete_read_only_source(self, resource_id: str, source_id: str) -> None:
        self.call("InstanceService", "DeleteDataSource", {
            "name": f"instances/{resource_id}/dataSources/{source_id}"})

    def full_sync(self, resource_ids: list[str]) -> None:
        self.call("InstanceService", "BatchSyncInstances", {
            "requests": [{"name": f"instances/{rid}", "enableFullSync": True} for rid in resource_ids]})

    def search_tables(self, parent_prefix: str, search: str = "") -> list[dict]:
        results, token = [], ""
        while True:
            payload = {"parentGuidPrefix": parent_prefix, "metaType": "TABLE",
                       "searchStr": search, "pageSize": 100}
            if token:
                payload["pageToken"] = token
            body = self.call("DatabaseService", "SearchMetadata", payload)
            results.extend(body.get("results", []))
            token = body.get("nextPageToken", "")
            if not token:
                return results

    def search_views(self, parent_prefix: str, search: str = "") -> list[dict]:
        body = self.call("DatabaseService", "SearchMetadata", {
            "parentGuidPrefix": parent_prefix, "metaType": "VIEW",
            "searchStr": search, "pageSize": 100})
        return body.get("results", [])

    def resolve_guids(self) -> dict[str, str]:
        """Resolve every fixture object once the registry has it (bounded poll)."""
        deadline = time.time() + WAIT_REGISTRY
        prefixes = (
            f"{MYSQL_INSTANCE};{MYSQL_DB}",
            f"{NOREAD_INSTANCE};{MYSQL_DB}",
            f"{PG_INSTANCE};{PG_DB}",
            f"{PG_INSTANCE};{PG_REF_DB}",
        )
        wanted = [MYSQL_GUID(t) for t in MYSQL_TABLES] + [
            MYSQL_VIEW_GUID, NOREAD_GUID, PG_GUID("dq_orders"), PG_GUID("dq_big"), PG_REF_GUID]
        while time.time() < deadline:
            guids = {}
            for prefix in prefixes:
                for row in self.search_tables(prefix, "dq_"):
                    guids[row["guid"]] = row.get("metaType", "TABLE")
            for row in self.search_views(f"{MYSQL_INSTANCE};{MYSQL_DB}", "dq_"):
                guids[row["guid"]] = "VIEW"
            missing = [g for g in wanted if g not in guids]
            if not missing:
                return guids
            time.sleep(1)
        raise SystemExit(f"registry never exposed these objects after {WAIT_REGISTRY:.0f}s: {missing}")

    def wait_until(self, predicate, label: str, timeout: float = WAIT_REGISTRY):
        """Poll until the registry (and the API reading it) reflects a change."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            value = predicate()
            if value:
                return value
            time.sleep(1)
        raise SystemExit(f"timed out after {timeout:.0f}s waiting for: {label}")

    def guid(self, table: str, kind: str = "mysql") -> str:
        if kind == "pg":
            return PG_GUID(table)
        if kind == "noread":
            return f"{NOREAD_INSTANCE};{MYSQL_DB};;{table}"
        return MYSQL_GUID(table)

    # ---- rules ----

    def list_rules(self, **filters) -> list[dict]:
        rules, token = [], ""
        while True:
            payload = {"parent": self.workspace, "pageSize": 200, **filters}
            if token:
                payload["pageToken"] = token
            body = self.call("DataQualityService", "ListQualityRules", payload)
            rules.extend(body.get("qualityRules", []))
            token = body.get("nextPageToken", "")
            if not token:
                return rules

    def batch_create_rules(self, specs: list[dict]) -> dict[str, str]:
        """Create every rule in one call; returns key -> resource name."""
        requests = [{"parent": self.workspace, "rule": rule} for _, rule in specs]
        body = self.call("DataQualityService", "BatchCreateQualityRules", {"parent": self.workspace, "requests": requests})
        names: dict[str, str] = {}
        for (key, _), result in zip(specs, body.get("results", []), strict=False):
            if result.get("error"):
                raise SystemExit(f"rule {key} was rejected: {result['error']}")
            names[key] = result["rule"]["name"]
        return names

    def create_rule(self, rule: dict) -> dict:
        return self.call("DataQualityService", "CreateQualityRule", {"parent": self.workspace, "rule": rule})

    def archive_rule(self, name: str) -> None:
        self.call("DataQualityService", "DeleteQualityRule", {"name": name})

    def run_rules(self, names: list[str]) -> dict[str, dict]:
        """Run one selection and wait for every execution; keyed by rule name."""
        body = self.call("DataQualityService", "RunQualityRules",
                         {"parent": self.workspace, "rules": names})
        executions = body.get("executions", [])
        by_rule = {ex["rule"]: ex for ex in executions}
        done: dict[str, dict] = {}
        deadline = time.time() + WAIT_EXECUTION
        pending = dict(by_rule)
        while pending and time.time() < deadline:
            for rule, ex in list(pending.items()):
                current = self.execution(ex["name"])
                if current.get("state") in ("QUALITY_EXECUTION_STATE_COMPLETED",
                                            "QUALITY_EXECUTION_STATE_ABANDONED"):
                    done[rule] = current
                    del pending[rule]
            if pending:
                time.sleep(0.4)
        if pending:
            raise SystemExit(f"executions did not finish in {WAIT_EXECUTION:.0f}s: {list(pending)}")
        return done

    def execution(self, name: str) -> dict:
        return self.call("DataQualityService", "GetQualityExecution", {"name": name})

    def list_executions(self, **filters) -> list[dict]:
        return self.call("DataQualityService", "ListQualityExecutions",
                         {"parent": self.workspace, "pageSize": 200, **filters}).get("qualityExecutions", [])

    def issues(self, **filters) -> list[dict]:
        out, token = [], ""
        while True:
            payload = {"parent": self.workspace, "pageSize": 200, **filters}
            if token:
                payload["pageToken"] = token
            body = self.call("DataQualityService", "ListQualityIssues", payload)
            out.extend(body.get("qualityIssues", []))
            token = body.get("nextPageToken", "")
            if not token:
                return out

    def resolve_issue(self, issue: dict, note: str) -> None:
        self.call("DataQualityService", "UpdateQualityIssue", {
            "issue": {"name": issue["name"], "status": "QUALITY_ISSUE_STATUS_RESOLVED",
                      "resolutionReason": "QUALITY_RESOLUTION_REASON_NO_ACTION_NEEDED",
                      "resolutionNote": note},
            "updateMask": "status,resolutionReason,resolutionNote",
        })

    # ---- reads ----

    def notifications(self) -> list[dict]:
        out, token = [], ""
        while True:
            payload = {"parent": self.workspace, "pageSize": 1000}
            if token:
                payload["pageToken"] = token
            body = self.call("NotificationService", "ListNotifications", payload)
            out.extend(body.get("notifications", []))
            token = body.get("nextPageToken", "")
            if not token:
                return out

    def channels(self) -> list[dict]:
        return self.call("NotificationService", "ListNotificationChannels",
                         {"parent": self.workspace}).get("notificationChannels", [])

    def deliveries(self, channel: str = "") -> list[dict]:
        payload = {"parent": self.workspace, "pageSize": 1000}
        if channel:
            payload["channel"] = channel
        return self.call("NotificationService", "ListNotificationDeliveries",
                         payload).get("notificationDeliveries", [])

    def target_summary(self, **filters) -> dict:
        body = self.call("DataQualityService", "ListQualityTargets",
                         {"parent": self.workspace, "pageSize": 100, **filters})
        by_guid = {row["target"]["metaGuid"]: row for row in body.get("qualityTargets", [])}
        return by_guid

    def get_target(self, guid: str, meta_type: str = "TABLE") -> dict:
        return self.call("DataQualityService", "GetQualityTarget",
                         {"parent": self.workspace, "targetGuid": guid, "metaType": meta_type})

    def overview(self) -> dict:
        return self.call("DataQualityService", "GetQualityOverview", {"parent": self.workspace})

    def schedule(self, guid: str, meta_type: str = "TABLE") -> dict:
        return self.call("DataQualityService", "GetQualityTargetSchedule",
                         {"parent": self.workspace, "targetGuid": guid, "metaType": meta_type})

    def report(self, guid: str, meta_type: str = "TABLE", language: str = "en-US") -> dict:
        return self.call("DataQualityService", "ExportQualityReport", {
            "parent": self.workspace, "targetGuid": guid,
            "metaType": meta_type, "language": language})

    def audit_logs(self, max_pages: int = 30) -> list[dict]:
        out, token = [], ""
        for _ in range(max_pages):
            payload = {"parent": "workspaces/-", "pageSize": 100}
            if token:
                payload["pageToken"] = token
            body = self.call("AuditLogService", "ListAuditLogs", payload)
            out.extend(body.get("auditLogs", []))
            token = body.get("nextPageToken", "")
            if not token:
                break
        return out


# ---- local fixtures --------------------------------------------------------


def run(cmd: list[str], stdin_path: Path | None = None, env: dict | None = None) -> None:
    full_env = dict(os.environ)
    full_env.update(env or {})
    with (open(stdin_path, "rb") if stdin_path else open(os.devnull, "rb")) as stdin:
        proc = subprocess.run(cmd, stdin=stdin, env=full_env, capture_output=True, text=True)
    if proc.returncode != 0:
        raise SystemExit(f"command failed ({proc.returncode}): {' '.join(cmd)}\n{proc.stderr[-3000:]}")


def mysql_file(name: str) -> None:
    run(["docker", "exec", "-i", MYSQL_CONTAINER, "mysql", "-uroot", f"-p{MYSQL_ROOT_PASSWORD}"],
        stdin_path=ROOT / "mysql" / name)


def mysql_sql(sql: str) -> None:
    run(["docker", "exec", "-i", MYSQL_CONTAINER, "mysql", "-uroot", f"-p{MYSQL_ROOT_PASSWORD}", "-e", sql])


def metadata_sql(sql: str) -> None:
    run(["psql", "-h", PG_HOST, "-p", PG_PORT, "-U", PG_USER, "-d", METADATA_DB, "-tAc", sql],
        env={"PGPASSWORD": PG_PASSWORD})


def pg_file(database: str, name: str) -> None:
    run(["psql", "-h", PG_HOST, "-p", PG_PORT, "-U", PG_USER, "-d", database, "-q", "-f", str(ROOT / "pg" / name)],
        env={"PGPASSWORD": PG_PASSWORD})


def pg_superuser(sql: str) -> str:
    proc = subprocess.run(
        ["docker", "exec", PG_CONTAINER, "psql", "-U", "postgres", "-d", "postgres", "-tAc", sql],
        capture_output=True, text=True)
    if proc.returncode != 0:
        raise SystemExit(f"postgres superuser command failed: {sql}\n{proc.stderr[-2000:]}")
    return proc.stdout.strip()


def pg_create_databases() -> None:
    for db in (PG_DB, PG_REF_DB):
        if pg_superuser(f"select 1 from pg_database where datname = '{db}'") == "1":
            continue
        pg_superuser(f"CREATE DATABASE {db} OWNER {PG_USER}")


# ---- rule payloads ---------------------------------------------------------


def load_rule_file(name: str) -> dict:
    return json.loads((ROOT / "rules" / name).read_text())


def build_rule(client: Client, spec: dict, kind: str, guids: dict[str, str]) -> dict:
    rule = json.loads(json.dumps(spec["rule"]))
    table_guid = client.guid(spec["table"], kind)
    if table_guid not in guids:
        raise SystemExit(f"{spec['key']}: registry has no {table_guid}")
    rule["target"] = {"metaGuid": table_guid, "metaType": spec.get("metaType", "TABLE")}
    config = rule.get("config", {})
    if "crossTable" in config:
        ref = config["crossTable"]["reference"]
        ref_table = ref.pop("table")
        ref_db = ref.pop("database", None)
        if ref_db:
            ref_guid = f"{PG_INSTANCE};{ref_db};{PG_DB};{ref_table}"
        else:
            ref_guid = client.guid(ref_table, kind)
        if ref_guid not in guids:
            raise SystemExit(f"{spec['key']}: registry has no reference {ref_guid}")
        ref["metaGuid"] = ref_guid
        ref["metaType"] = ref.pop("metaType", "TABLE")
    return rule


# ---- assertions ------------------------------------------------------------


def check_expectation(rep: Reporter, key: str, execution: dict, expect: dict) -> None:
    if "outcome" in expect:
        rep.equal(execution.get("outcome"), expect["outcome"], f"{key} outcome")
    if "failedRows" in expect:
        got = execution.get("failedRows")
        rep.check(got is not None and int(got) == expect["failedRows"], f"{key} failed rows",
                  f"got {got}, want {expect['failedRows']}")
    if "totalRows" in expect:
        got = execution.get("totalRows")
        rep.check(got is not None and int(got) == expect["totalRows"], f"{key} total rows",
                  f"got {got}, want {expect['totalRows']}")
    if "observedValue" in expect:
        got = execution.get("observedValue")
        want = expect["observedValue"]
        rep.check(got is not None and abs(float(got) - float(want)) < 1e-6, f"{key} observed value",
                  f"got {got}, want {want}")
    if "sampled" in expect:
        rep.equal(bool(execution.get("sampled")), expect["sampled"], f"{key} sampled")
    if "errorCode" in expect:
        rep.equal(execution.get("errorCode"), expect["errorCode"], f"{key} error code")
    if "executedSqlContains" in expect:
        sql = execution.get("executedSql", "")
        for fragment in expect["executedSqlContains"]:
            rep.check(fragment in sql, f"{key} executed SQL contains {fragment!r}", sql[:200])
    if "referenceExecutedSqlContains" in expect:
        sql = execution.get("referenceExecutedSql", "")
        for fragment in expect["referenceExecutedSqlContains"]:
            rep.check(fragment in sql, f"{key} reference SQL contains {fragment!r}", sql[:200])


def parse_time(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def dq_details(notification: dict) -> dict | None:
    return notification.get("dataQuality")


def check_issue_open(rep: Reporter, client: Client, rule_name: str, key: str) -> dict | None:
    issues = client.issues(rule=rule_name)
    open_issues = [i for i in issues if i.get("status") != "QUALITY_ISSUE_STATUS_RESOLVED"]
    rep.equal(len(open_issues), 1, f"{key} opened exactly one issue")
    if not open_issues:
        return None
    issue = open_issues[0]
    rep.equal(issue.get("status"), "QUALITY_ISSUE_STATUS_OPEN", f"{key} issue is OPEN")
    return issue


def check_issue_recovered(rep: Reporter, client: Client, rule_name: str, key: str) -> None:
    issues = client.issues(rule=rule_name)
    resolved = [i for i in issues if i.get("status") == "QUALITY_ISSUE_STATUS_RESOLVED"]
    rep.equal(len(resolved), 1, f"{key} issue resolved exactly once")
    if resolved:
        rep.equal(resolved[0].get("resolutionReason"),
                  "QUALITY_RESOLUTION_REASON_AUTO_RECOVERED", f"{key} resolved AUTO_RECOVERED")


def rule_of(notification: dict) -> str:
    detail = dq_details(notification) or {}
    return detail.get("rule", "")


def kind_of(notification: dict) -> str:
    kind = (dq_details(notification) or {}).get("kind", "")
    return kind.removeprefix("DATA_QUALITY_NOTIFICATION_KIND_")


# ---- fixture reset ---------------------------------------------------------


def fixture_guids() -> list[str]:
    return ([MYSQL_GUID(t) for t in MYSQL_TABLES] + [MYSQL_VIEW_GUID, NOREAD_GUID,
                                                     PG_GUID("dq_orders"), PG_GUID("dq_big"), PG_REF_GUID])


def reset_fixture_state(client: Client) -> None:
    """Close leftover issues, then archive fixture rules from an earlier run."""
    for guid in fixture_guids():
        for issue in client.issues(target_guid=guid):
            if issue.get("status") != "QUALITY_ISSUE_STATUS_RESOLVED":
                client.resolve_issue(issue, "e2e fixture reset")
    for instance, database in ((MYSQL_INSTANCE, MYSQL_DB), (NOREAD_INSTANCE, MYSQL_DB),
                               (PG_INSTANCE, PG_DB), (PG_INSTANCE, PG_REF_DB)):
        for rule in client.list_rules(instance=instance, database=database):
            if rule.get("state") != "QUALITY_RULE_STATE_ARCHIVED":
                client.archive_rule(rule["name"])


# ---- scenarios -------------------------------------------------------------


def scenario_gates(client: Client, rep: Reporter) -> None:
    rep.section("S1 save-time gates (no execution)")

    def base_rule(table: str, rule_type: str, config: dict) -> dict:
        return {
            "title": f"gate probe {rule_type}",
            "dimension": "QUALITY_DIMENSION_VALIDITY",
            "ruleType": rule_type,
            "severity": "QUALITY_SEVERITY_ERROR",
            "target": {"metaGuid": client.guid(table), "metaType": "TABLE"},
            "config": config,
        }

    # Config arm that does not match the rule type.
    try:
        client.create_rule(base_rule("dq_orders", "QUALITY_RULE_TYPE_ROW_COUNT",
                                     {"range": {"column": "amount", "min": "0"}}))
        rep.check(False, "mismatched config arm is rejected")
    except ApiError as exc:
        rep.equal(exc.code, "invalid_argument", "mismatched config arm is rejected")

    # A view only allows table-level rules.
    try:
        client.create_rule({
            "title": "gate probe view range",
            "dimension": "QUALITY_DIMENSION_VALIDITY",
            "ruleType": "QUALITY_RULE_TYPE_RANGE",
            "severity": "QUALITY_SEVERITY_ERROR",
            "target": {"metaGuid": MYSQL_VIEW_GUID, "metaType": "VIEW"},
            "config": {"range": {"column": "amount", "min": "0"}},
        })
        rep.check(False, "range on a view is rejected")
    except ApiError as exc:
        rep.equal(exc.code, "invalid_argument", "range on a view is rejected")

    # Unknown target GUID.
    try:
        client.create_rule({
            "title": "gate probe unknown target",
            "dimension": "QUALITY_DIMENSION_VALIDITY",
            "ruleType": "QUALITY_RULE_TYPE_ROW_COUNT",
            "severity": "QUALITY_SEVERITY_ERROR",
            "target": {"metaGuid": f"{MYSQL_INSTANCE};e2e_dq;;does_not_exist", "metaType": "TABLE"},
            "config": {"rowCount": {"minRows": "1"}},
        })
        rep.check(False, "unknown target GUID is rejected")
    except ApiError as exc:
        rep.equal(exc.code, "not_found", "unknown target GUID is rejected")

    # A stale revision is a conflict, not a silent overwrite. The client echoes
    # the revision it read; the server compares it with the head row.
    temp = client.create_rule(base_rule("dq_groups", "QUALITY_RULE_TYPE_ROW_COUNT",
                                        {"rowCount": {"minRows": "0"}}))
    try:
        client.call("DataQualityService", "UpdateQualityRule", {
            "rule": {"name": temp["name"], "title": "gate probe stale revision",
                     "dimension": "QUALITY_DIMENSION_ACCURACY",
                     "ruleType": "QUALITY_RULE_TYPE_ROW_COUNT",
                     "severity": "QUALITY_SEVERITY_ERROR",
                     "target": {"metaGuid": client.guid("dq_groups"), "metaType": "TABLE"},
                     "config": {"rowCount": {"minRows": "1"}},
                     "revision": 99},
            "updateMask": "title,config",
        })
        rep.check(False, "stale expected revision is rejected")
    except ApiError as exc:
        rep.equal(exc.code, "aborted", "stale expected revision is rejected")

    # Archiving keeps history readable: run the rule once, archive it, then read.
    client.run_rules([temp["name"]])
    client.archive_rule(temp["name"])
    archived = client.call("DataQualityService", "GetQualityRule", {"name": temp["name"]})
    rep.equal(archived.get("state"), "QUALITY_RULE_STATE_ARCHIVED", "archived rule keeps state")
    rep.check(len(client.list_executions(rule=temp["name"])) >= 1,
              "archived rule history is readable")

    # PERCENTILE is a PostgreSQL/MSSQL-only aggregate.
    try:
        client.create_rule({
            "title": "gate probe percentile on mysql",
            "dimension": "QUALITY_DIMENSION_ACCURACY",
            "ruleType": "QUALITY_RULE_TYPE_AGGREGATE",
            "severity": "QUALITY_SEVERITY_ERROR",
            "target": {"metaGuid": client.guid("dq_orders"), "metaType": "TABLE"},
            "config": {"aggregate": {"column": "amount", "function": "QUALITY_AGGREGATE_FUNCTION_PERCENTILE",
                                     "percentile": 50, "min": 0}},
        })
        rep.check(False, "PERCENTILE is rejected on MySQL")
    except ApiError as exc:
        rep.equal(exc.code, "invalid_argument", "PERCENTILE is rejected on MySQL")

    # jsonb has no comparison capability, so a range rule cannot be saved on it.
    try:
        client.create_rule({
            "title": "gate probe range on jsonb",
            "dimension": "QUALITY_DIMENSION_VALIDITY",
            "ruleType": "QUALITY_RULE_TYPE_RANGE",
            "severity": "QUALITY_SEVERITY_ERROR",
            "target": {"metaGuid": client.guid("dq_orders", "pg"), "metaType": "TABLE"},
            "config": {"range": {"column": "payload", "min": "0"}},
        })
        rep.check(False, "range on jsonb is rejected")
    except ApiError as exc:
        rep.equal(exc.code, "invalid_argument", "range on jsonb is rejected")


def scenario_matrix(client: Client, rep: Reporter, rules: list[dict],
                    names: dict[str, str]) -> None:
    rep.section("S2 verdict matrix (MySQL)")
    by_key = {spec["key"]: spec for spec in rules}

    batches: dict[str, list[str]] = {}
    for spec in rules:
        batches.setdefault(spec["batch"], []).append(spec["key"])
    ran: dict[str, dict] = {}
    for batch, keys in batches.items():
        if batch in ("row-count-change", "segment", "drift", "orphan"):
            continue
        results = client.run_rules([names[k] for k in keys])
        for key in keys:
            execution = results[names[key]]
            ran[key] = execution
            rep.equal(execution.get("state"), "QUALITY_EXECUTION_STATE_COMPLETED", f"{key} completed")
            rep.equal(execution.get("ruleRevision"), 1, f"{key} ran revision 1")
            check_expectation(rep, key, execution, by_key[key]["expect"])

    # Failure samples: a duplicate check keeps the grouping key, a row check
    # keeps the primary key plus the checked column, and NULL is not "".
    sample_exec = ran["orders-unique"]
    samples = sample_exec.get("samples") or []
    rep.check(len(samples) >= 1, "failure samples are recorded")
    if samples:
        column_names = [value.get("column") for value in samples[0].get("columns", [])]
        rep.check(column_names == ["external_ref"], "a duplicate sample is the grouping key",
                  str(column_names))
    row_samples = ran["orders-accepted-values"].get("samples") or []
    rep.check(len(row_samples) >= 1, "a row-level failure sample is recorded")
    if row_samples:
        row_columns = [value.get("column") for value in row_samples[0].get("columns", [])]
        rep.check("id" in row_columns and "status" in row_columns,
                  "the row sample carries the primary key and the checked column", str(row_columns))
    nulls = [value for sample in (ran["orders-null-percent"].get("samples") or [])
             for value in sample.get("columns", []) if value.get("isNull")]
    rep.check(bool(nulls), "a sampled SQL NULL is marked is_null")

    # Each run creates one batch for its single target, and the rollup matches.
    run = client.call("DataQualityService", "GetQualityRun", {"name": sample_exec["run"]})
    rep.equal(run.get("state"), "QUALITY_EXECUTION_STATE_COMPLETED", "batch is COMPLETED")
    rep.equal(run.get("trigger"), "QUALITY_EXECUTION_TRIGGER_MANUAL", "batch trigger is MANUAL")
    rep.check(run.get("counts", {}).get("total", 0) >= 1, "batch counts its executions")
    rep.equal(run.get("target", {}).get("metaGuid"), MYSQL_GUID("dq_orders"), "batch belongs to one target")

    # The first rule of a target seeds its schedule from the workspace default.
    schedule = client.schedule(MYSQL_GUID("dq_orders"))
    interval = schedule.get("scheduleInterval", "")
    rep.check(interval.endswith("s") and float(interval[:-1] or 0) >= 900,
              "target schedule has a valid interval (>= 15m)", interval)
    rep.check(bool(schedule.get("nextRunTime")), "target schedule has a next run")

    # Every FAIL opened exactly one issue and one in-app notification.
    notifications = client.notifications()
    for key, execution in ran.items():
        if execution.get("outcome") != "QUALITY_STATUS_FAIL":
            continue
        check_issue_open(rep, client, names[key], key)
        opened = [n for n in notifications
                  if kind_of(n) == "ISSUE_OPENED" and rule_of(n) == names[key]]
        rep.check(len(opened) >= 1, f"{key} sent ISSUE_OPENED")
    return ran


def scenario_row_count_change(client: Client, rep: Reporter, names: dict[str, str]) -> None:
    rep.section("S3 row_count_change baseline")
    name = names["orders-row-count-change"]

    first = client.run_rules([name])[name]
    rep.equal(first.get("outcome"), "QUALITY_STATUS_PASS", "first run records the baseline and passes")
    second = client.run_rules([name])[name]
    rep.equal(second.get("outcome"), "QUALITY_STATUS_PASS", "second run compares against the baseline")
    rep.check(bool(second.get("baselineValue")), "second run carries the baseline value")

    mysql_file("30_row_count_change_growth.sql")
    third = client.run_rules([name])[name]
    rep.equal(third.get("outcome"), "QUALITY_STATUS_FAIL", "a 50% increase fails a 20% bound")
    rep.check(third.get("baselineValue") is not None and float(third["baselineValue"]) == 10,
              "failed run records the 10-row baseline", str(third.get("baselineValue")))

    fourth = client.run_rules([name])[name]
    rep.equal(fourth.get("outcome"), "QUALITY_STATUS_PASS", "unchanged row count passes against the new baseline")

    mysql_file("40_row_count_change_revert.sql")
    check_issue_recovered(rep, client, name, "orders-row-count-change")


def scenario_segment(client: Client, rep: Reporter, names: dict[str, str]) -> None:
    rep.section("S4 segment_check across runs")
    name = names["segment-segment-check"]

    first = client.run_rules([name])[name]
    rep.equal(first.get("outcome"), "QUALITY_STATUS_NOT_EVALUATED", "first run records the baseline only")
    evidence = first.get("segmentEvidence") or {}
    rep.check(bool(evidence.get("observed")), "first run stores the observed segments")

    mysql_file("80_segment_lose_region.sql")
    second = client.run_rules([name])[name]
    rep.equal(second.get("outcome"), "QUALITY_STATUS_FAIL", "a disappeared segment fails")
    rep.equal(second.get("failedRows"), "1" if isinstance(second.get("failedRows"), str) else 1,
              "exactly one disappeared segment")
    disappeared = (second.get("segmentEvidence") or {}).get("disappeared") or []
    rep.equal(len(disappeared), 1, "the evidence names the disappeared segment")
    check_issue_open(rep, client, name, "segment-segment-check")

    mysql_file("81_segment_restore_region.sql")
    third = client.run_rules([name])[name]
    rep.equal(third.get("outcome"), "QUALITY_STATUS_PASS", "restoring the segment passes")
    check_issue_recovered(rep, client, name, "segment-segment-check")


def scenario_repair(client: Client, rep: Reporter, rules: list[dict], names: dict[str, str],
                    opened: set[str]) -> None:
    rep.section("S5 repair and auto-recovery (MySQL)")
    mysql_file("50_repair.sql")

    for batch in ("orders-a", "orders-b", "groups", "heartbeat"):
        keys = [spec["key"] for spec in rules if spec["batch"] == batch]
        results = client.run_rules([names[k] for k in keys])
        for key in keys:
            execution = results[names[key]]
            rep.equal(execution.get("outcome"), "QUALITY_STATUS_PASS", f"{key} passes after repair")
            if key in opened:
                check_issue_recovered(rep, client, names[key], key)

    # The overview now reports a score and coverage for the fixture.
    overview = client.overview()
    rep.check(overview.get("qualityScore") is not None, "workspace quality score is set after judging")
    rep.check(overview.get("monitoredTableCount", 0) >= 8, "overview counts the monitored targets",
              str(overview.get("monitoredTableCount")))
    instance_rows = {row.get("key"): row for row in overview.get("coverageByInstance", [])}
    rep.check(MYSQL_INSTANCE in instance_rows and instance_rows[MYSQL_INSTANCE].get("monitoredTableCount", 0) >= 8,
              "coverage is broken down by instance")

    # The trend read returns both the failing and the passing point.
    trend = client.call("DataQualityService", "GetQualityRuleTrend",
                        {"name": names["orders-unique"]})
    rep.check(len(trend.get("points", [])) >= 2, "trend has the failing and the passing point")

    # The workspace directory and the workspace page agree on the target.
    summary = client.target_summary(instance=MYSQL_INSTANCE, database=MYSQL_DB)
    orders = summary.get(MYSQL_GUID("dq_orders"))
    rep.check(orders is not None, "target directory lists dq_orders")
    if orders:
        rep.equal(orders.get("enabledRuleCount"), DQ_ORDERS_RULE_COUNT,
                  f"dq_orders has {DQ_ORDERS_RULE_COUNT} enabled checks")
        rep.equal(orders.get("openIssueCount", 0), 0, "dq_orders has no open issue after repair")
        rep.check(orders.get("qualityScore") is not None, "target quality score is set")
        rep.check(0 < orders.get("coveredColumnCount", 0) <= orders.get("totalColumnCount", 0),
                  "covered columns never exceed the column count")
        rep.equal(orders.get("totalColumnCount"), DQ_ORDERS_COLUMN_COUNT, "dq_orders column count")

    target = client.get_target(MYSQL_GUID("dq_orders"))
    rep.check(len(target.get("ruleHistory", [])) >= 1, "target workspace returns rule history")
    rep.equal(len(target.get("targetColumns", [])), DQ_ORDERS_COLUMN_COUNT,
              "target workspace returns the current columns")
    rep.check(target.get("summary", {}).get("latestRun") is not None, "target summary carries its latest run")


def scenario_schema_drift(client: Client, rep: Reporter, names: dict[str, str],
                          spec: dict) -> None:
    rep.section("S6 schema_drift and schema-change acknowledgement")
    name = names["drift-schema-drift"]

    rep.equal(client.run_rules([name])[name].get("outcome"), "QUALITY_STATUS_PASS",
              "schema_drift passes before the DDL change")

    mysql_file("60_schema_drift_alter.sql")
    client.full_sync([MYSQL_INSTANCE])
    client.wait_until(lambda: len(client.get_target(MYSQL_GUID("dq_drift_target"))
                                  .get("targetColumns", [])) == DRIFT_COLUMNS_AFTER,
                      "the registry to see the added column")
    failed = client.run_rules([name])[name]
    check_expectation(rep, "drift-schema-drift", failed, spec["expect"])
    rep.equal(failed.get("executedSql", None), None, "metadata rules run no SQL")
    check_issue_open(rep, client, name, "drift-schema-drift")

    rule = client.call("DataQualityService", "GetQualityRule", {"name": name})
    rep.check(bool(rule.get("targetSchemaChanged")), "the rule reports target_schema_changed")
    summary = client.get_target(MYSQL_GUID("dq_drift_target")).get("summary", {})
    rep.check(bool(summary.get("schemaChanged")), "the target summary reports schema_changed")

    client.call("DataQualityService", "AcknowledgeQualityTargetSchema", {
        "parent": client.workspace, "targetGuid": MYSQL_GUID("dq_drift_target"), "metaType": "TABLE"})
    acknowledged = client.call("DataQualityService", "GetQualityRule", {"name": name})
    rep.check(not acknowledged.get("targetSchemaChanged"), "acknowledgement clears target_schema_changed")

    mysql_file("61_schema_drift_revert.sql")
    client.full_sync([MYSQL_INSTANCE])
    client.wait_until(lambda: len(client.get_target(MYSQL_GUID("dq_drift_target"))
                                  .get("targetColumns", [])) == DRIFT_COLUMNS_BEFORE,
                      "the registry to forget the dropped column")
    rep.equal(client.run_rules([name])[name].get("outcome"), "QUALITY_STATUS_PASS",
              "reverting the DDL passes again")
    check_issue_recovered(rep, client, name, "drift-schema-drift")


def scenario_orphan(client: Client, rep: Reporter, names: dict[str, str], spec: dict) -> None:
    rep.section("S7 orphaned target")
    name = names["orphan-row-count"]
    mysql_file("70_orphan_drop.sql")
    client.full_sync([MYSQL_INSTANCE])
    client.wait_until(lambda: all(row["guid"] != MYSQL_GUID("dq_orphan_target")
                                  for row in client.search_tables(f"{MYSQL_INSTANCE};{MYSQL_DB}", "dq_orphan")),
                      "the registry to drop the orphaned table")
    execution = client.run_rules([name])[name]
    check_expectation(rep, "orphan-row-count", execution, spec["expect"])
    rep.equal(len([i for i in client.issues(rule=name)
                   if i.get("status") != "QUALITY_ISSUE_STATUS_RESOLVED"]), 0,
              "a SKIPPED run opens no issue")


def scenario_error_path(client: Client, rep: Reporter) -> None:
    rep.section("S8 missing READ_ONLY source")

    # A dedicated instance: deleting its source cannot affect anything else, and
    # the fixture re-creates it before every run.
    probe = client.create_rule({
        "title": "e2e no-read-only probe",
        "dimension": "QUALITY_DIMENSION_ACCURACY",
        "ruleType": "QUALITY_RULE_TYPE_ROW_COUNT",
        "severity": "QUALITY_SEVERITY_ERROR",
        "target": {"metaGuid": NOREAD_GUID, "metaType": "TABLE"},
        "config": {"rowCount": {"minRows": "1"}},
    })

    client.delete_read_only_source(NOREAD_INSTANCE, READER_SOURCE_ID)

    try:
        client.create_rule({
            "title": "gate probe no read-only source",
            "dimension": "QUALITY_DIMENSION_ACCURACY",
            "ruleType": "QUALITY_RULE_TYPE_ROW_COUNT",
            "severity": "QUALITY_SEVERITY_ERROR",
            "target": {"metaGuid": NOREAD_GUID, "metaType": "TABLE"},
            "config": {"rowCount": {"minRows": "1"}},
        })
        rep.check(False, "creating a query rule without a READ_ONLY source is rejected")
    except ApiError as exc:
        rep.equal(exc.code, "failed_precondition", "creating a query rule without a READ_ONLY source is rejected")

    execution = client.run_rules([probe["name"]])[probe["name"]]
    rep.equal(execution.get("outcome"), "QUALITY_STATUS_ERROR", "running without a READ_ONLY source is ERROR")
    rep.equal(execution.get("errorCode"), "NO_READ_ONLY_SOURCE", "the error code names the cause")
    rep.equal(len(client.issues(rule=probe["name"])), 0, "an ERROR opens no issue")
    failed_notifications = [n for n in client.notifications()
                            if kind_of(n) == "EXECUTION_FAILED" and rule_of(n) == probe["name"]]
    rep.check(len(failed_notifications) >= 1, "EXECUTION_FAILED is sent to the administrators")

    client.ensure_read_only_source(NOREAD_INSTANCE, MYSQL_HOST, MYSQL_PORT,
                                   READER_USER, READER_PASSWORD, READER_SOURCE_ID)
    rep.equal(client.run_rules([probe["name"]])[probe["name"]].get("outcome"), "QUALITY_STATUS_PASS",
              "restoring the source passes again")
    client.archive_rule(probe["name"])


def scenario_scheduled_tick(client: Client, rep: Reporter, names: dict[str, str]) -> None:
    rep.section("S9 scheduled tick (one metadata-DB seam)")
    guid = MYSQL_GUID("dq_heartbeat")
    before = client.schedule(guid)
    rep.check(bool(before.get("nextRunTime")), "the target starts with a next run")
    started = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    metadata_sql(
        "UPDATE quality_target_schedule "
        f"SET next_run_at = NOW() - INTERVAL '1 minute', updated_at = NOW() "
        f"WHERE target_guid = '{guid}' AND target_type = {TARGET_TYPE_TABLE}")

    deadline = time.time() + 120
    execution = None
    while time.time() < deadline and execution is None:
        candidates = client.list_executions(
            trigger="QUALITY_EXECUTION_TRIGGER_SCHEDULED", targetGuid=guid,
            rule=names["heartbeat-freshness"], startTime=started, pageSize=10)
        for candidate in candidates:
            if candidate.get("state") in ("QUALITY_EXECUTION_STATE_COMPLETED",
                                          "QUALITY_EXECUTION_STATE_ABANDONED"):
                execution = client.execution(candidate["name"])
                break
        if execution is None:
            time.sleep(1)
    rep.check(execution is not None, "the runner picked the due target up")
    if execution:
        rep.equal(execution.get("trigger"), "QUALITY_EXECUTION_TRIGGER_SCHEDULED", "trigger is SCHEDULED")
        rep.equal(execution.get("initiator", ""), "", "a scheduled run has no initiator")
        rep.equal(execution.get("attempt"), 1, "first scheduled attempt")
        rep.equal(execution.get("outcome"), "QUALITY_STATUS_PASS", "the scheduled run passed")
        run = client.call("DataQualityService", "GetQualityRun", {"name": execution["run"]})
        rep.equal(run.get("trigger"), "QUALITY_EXECUTION_TRIGGER_SCHEDULED", "the batch trigger is SCHEDULED")
    after = client.schedule(guid)
    advanced = after.get("nextRunTime", "")
    rep.check(bool(advanced) and parse_time(advanced) > datetime.now(timezone.utc),
              "the schedule advanced to a future run", f"{before.get('nextRunTime')} -> {advanced}")


class WebhookReceiver:
    """Record every POST the server's outbound dispatcher makes."""

    def __init__(self) -> None:
        self.requests: list[dict] = []
        receiver = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802 - stdlib naming
                length = int(self.headers.get("Content-Length", "0"))
                body = self.rfile.read(length) if length else b""
                receiver.requests.append({"path": self.path, "body": body.decode(errors="replace")})
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"{}")

            def log_message(self, *_args) -> None:  # keep the output quiet
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.port = self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self) -> "WebhookReceiver":
        self.thread.start()
        return self

    def __exit__(self, *_exc) -> None:
        self.server.shutdown()
        self.server.server_close()


def scenario_webhook(client: Client, rep: Reporter) -> None:
    rep.section("S10 outbound webhook")
    for existing in client.channels():
        if existing.get("displayName", "").startswith("e2e-dq-"):
            client.call("NotificationService", "DeleteNotificationChannel", {"name": existing["name"]})

    with WebhookReceiver() as receiver:
        subscribed = client.call("NotificationService", "CreateNotificationChannel", {
            "parent": client.workspace,
            "channel": {
                "displayName": "e2e-dq-webhook",
                "kind": "NOTIFICATION_CHANNEL_KIND_WEBHOOK",
                "enabled": True,
                "eventTypes": ["NOTIFICATION_TYPE_DATA_QUALITY"],
                "bypassQuietHours": True,
                "config": {"webhook": {"sign": False}},
            },
            "secret": {"endpoint": f"http://127.0.0.1:{receiver.port}/"},
        })
        other_type = client.call("NotificationService", "CreateNotificationChannel", {
            "parent": client.workspace,
            "channel": {
                "displayName": "e2e-dq-other-type",
                "kind": "NOTIFICATION_CHANNEL_KIND_WEBHOOK",
                "enabled": True,
                "eventTypes": ["NOTIFICATION_TYPE_SCHEMA_SYNC"],
                "bypassQuietHours": True,
                "config": {"webhook": {"sign": False}},
            },
            "secret": {"endpoint": f"http://127.0.0.1:{receiver.port}/"},
        })

        rule = client.create_rule({
            "title": "e2e webhook probe (empty table fails)",
            "dimension": "QUALITY_DIMENSION_COMPLETENESS",
            "ruleType": "QUALITY_RULE_TYPE_ROW_COUNT",
            "severity": "QUALITY_SEVERITY_ERROR",
            "target": {"metaGuid": MYSQL_GUID("dq_empty"), "metaType": "TABLE"},
            "config": {"onEmpty": "QUALITY_EMPTY_BEHAVIOR_FAIL", "rowCount": {"minRows": "1"}},
        })
        execution = client.run_rules([rule["name"]])[rule["name"]]
        rep.equal(execution.get("outcome"), "QUALITY_STATUS_FAIL", "the webhook probe fails on an empty table")

        deadline = time.time() + 60
        sent = []
        while time.time() < deadline:
            sent = [d for d in client.deliveries(channel=subscribed["name"])
                    if d.get("state") == "NOTIFICATION_DELIVERY_STATE_SENT"]
            if sent:
                break
            time.sleep(1)
        rep.check(bool(sent), "the subscribed channel delivered the event")

        deadline = time.time() + 30
        while time.time() < deadline and not receiver.requests:
            time.sleep(0.5)
        rep.check(bool(receiver.requests), "the webhook endpoint received a POST")
        if receiver.requests:
            rep.check("quality" in receiver.requests[0]["body"].lower()
                      or "dq_empty" in receiver.requests[0]["body"],
                      "the webhook body names the event", receiver.requests[0]["body"][:300])

        other_deliveries = client.deliveries(channel=other_type["name"])
        rep.equal(len(other_deliveries), 0, "a channel that does not subscribe to DQ events gets nothing")

        for issue in client.issues(rule=rule["name"]):
            if issue.get("status") != "QUALITY_ISSUE_STATUS_RESOLVED":
                client.resolve_issue(issue, "e2e fixture cleanup")
        client.archive_rule(rule["name"])
        for channel in (subscribed, other_type):
            client.call("NotificationService", "DeleteNotificationChannel", {"name": channel["name"]})


def scenario_report(client: Client, rep: Reporter) -> None:
    rep.section("S11 single-target report and its audit row")
    report = client.report(MYSQL_GUID("dq_orders"))
    html = report.get("htmlContent", "")
    rep.check(report.get("fileName", "").endswith(".html"), "the report is an HTML file")
    rep.check("<html" in html.lower(), "the report is a complete HTML document")
    rep.check("dq_orders" in html, "the report names the target")
    rep.check("<script" not in html.lower(), "the report has no script tag")
    rep.check('src="http' not in html and "href=\"http" not in html,
              "the report references no external resource")

    logs = client.audit_logs()
    reports = [log for log in logs if "ExportQualityReport" in log.get("method", "")]
    rep.check(bool(reports), "the report call is audited")
    if reports:
        response = reports[0].get("response") or {}
        rep.check("htmlContent" not in response or not str(response.get("htmlContent", "")).startswith("<"),
                  "the audit row redacts the report body")
        request = reports[0].get("request") or {}
        rep.check("targetGuid" in request or "target_guid" in request,
                  "the audit row keeps the request")


def scenario_postgres(client: Client, rep: Reporter, names: dict[str, str]) -> None:
    rep.section("S12 PostgreSQL semantics")
    keys = ["pg-orders-range-sampled", "pg-orders-aggregate-percentile",
            "pg-orders-null-percent", "pg-orders-cross-table"]
    results = client.run_rules([names[k] for k in keys])
    specs = {spec["key"]: spec for spec in load_rule_file("postgres.json")["rules"]}
    for key in keys:
        execution = results[names[key]]
        rep.equal(execution.get("target", {}).get("metaGuid"),
                  client.guid(specs[key]["table"], "pg"),
                  f"{key} targets the schema-qualified GUID")
        check_expectation(rep, key, execution, specs[key]["expect"])

    sampled = results[names["pg-orders-range-sampled"]]
    rep.check(bool(sampled.get("sampled")), "percent sampling marks the execution sampled")
    rep.check(int(sampled.get("sampleRows") or 0) > 0, "percent sampling records the sampled row count")

    cross = results[names["pg-orders-cross-table"]]
    rep.check(bool(cross.get("referenceExecutedSql")),
              "a cross-database reconciliation records the reference statement",
              cross.get("referenceExecutedSql", "")[:200])

    check_issue_open(rep, client, names["pg-orders-range-sampled"], "pg-orders-range-sampled")
    check_issue_open(rep, client, names["pg-orders-null-percent"], "pg-orders-null-percent")

    # Repair and rerun: the recovered side proves the same lifecycle on PostgreSQL.
    pg_repair_sql = (
        f"UPDATE {PG_DB}.dq_orders SET customer_email = 'repaired@example.com' "
        "WHERE customer_email IS NULL; "
        f"UPDATE {PG_DB}.dq_big SET amount = 500.00")
    run(["psql", "-h", PG_HOST, "-p", PG_PORT, "-U", PG_USER, "-d", PG_DB, "-q", "-c", pg_repair_sql],
        env={"PGPASSWORD": PG_PASSWORD})
    recovered = client.run_rules([names["pg-orders-null-percent"], names["pg-orders-range-sampled"]])
    for key in ("pg-orders-null-percent", "pg-orders-range-sampled"):
        rep.equal(recovered[names[key]].get("outcome"), "QUALITY_STATUS_PASS",
                  f"PostgreSQL repair turns {key} PASS")
        check_issue_recovered(rep, client, names[key], key)


# ---- commands --------------------------------------------------------------


def cmd_bootstrap(client: Client) -> int:
    print("== bootstrap MySQL fixture")
    mysql_file("10_schema.sql")
    mysql_file("20_seed.sql")
    # CREATE IF NOT EXISTS would keep a stale password from an earlier checkout,
    # so the ALTER makes the fixture's reader credential authoritative.
    mysql_sql(
        f"CREATE USER IF NOT EXISTS '{READER_USER}'@'%' IDENTIFIED BY '{READER_PASSWORD}'; "
        f"ALTER USER '{READER_USER}'@'%' IDENTIFIED BY '{READER_PASSWORD}'; "
        f"GRANT SELECT ON {MYSQL_DB}.* TO '{READER_USER}'@'%'; FLUSH PRIVILEGES;")

    print("== bootstrap PostgreSQL fixture")
    pg_create_databases()
    pg_file(PG_DB, "10_schema.sql")
    pg_file(PG_DB, "20_seed.sql")
    pg_file(PG_REF_DB, "30_ref_schema.sql")
    pg_file(PG_REF_DB, "40_ref_seed.sql")

    print("== ensure instances and READ_ONLY sources")
    client.ensure_read_only_source(MYSQL_INSTANCE, MYSQL_HOST, MYSQL_PORT,
                                   READER_USER, READER_PASSWORD, READER_SOURCE_ID)
    # The fixture user, not root: the instance metadata stores this credential,
    # and a SELECT-only login is enough to sync the e2e_dq database.
    client.ensure_instance(NOREAD_INSTANCE, "dq-noread (fixture, no read-only source)",
                           MYSQL_HOST, MYSQL_PORT, READER_USER, READER_PASSWORD)
    client.ensure_read_only_source(NOREAD_INSTANCE, MYSQL_HOST, MYSQL_PORT,
                                   READER_USER, READER_PASSWORD, READER_SOURCE_ID)
    pg = client.instance(PG_INSTANCE)
    if not any(source.get("type") == "READ_ONLY" for source in pg.get("dataSources", [])):
        env_user = os.environ.get("DQ_PG_READER_USER", PG_USER)
        env_password = os.environ.get("DQ_PG_READER_PASSWORD", PG_PASSWORD)
        client.ensure_read_only_source(PG_INSTANCE, PG_HOST, PG_PORT, env_user, env_password,
                                       "e2e-dq-readonly")

    print("== full sync")
    client.full_sync([MYSQL_INSTANCE, NOREAD_INSTANCE, PG_INSTANCE])
    guids = client.resolve_guids()
    for guid in sorted(guids):
        print(f"   {guid}")
    print("\nbootstrap complete")
    return 0


def cmd_run(client: Client) -> int:
    rep = Reporter()
    mysql_rules = load_rule_file("mysql.json")["rules"]
    pg_rules = load_rule_file("postgres.json")["rules"]

    client.login()
    guids = client.resolve_guids()
    reset_fixture_state(client)

    rep.section("S0 create the rule suite")
    mysql_specs = [(spec["key"], build_rule(client, spec, "mysql", guids)) for spec in mysql_rules]
    pg_specs = [(spec["key"], build_rule(client, spec, "pg", guids)) for spec in pg_rules]
    names = client.batch_create_rules(mysql_specs + pg_specs)
    rep.equal(len(names), len(mysql_specs) + len(pg_specs), "every rule was created")
    rep.equal(len(client.list_rules(instance=MYSQL_INSTANCE, database=MYSQL_DB)),
              len(mysql_rules), "the MySQL fixture owns exactly its rule set")

    scenario_gates(client, rep)
    ran = scenario_matrix(client, rep, mysql_rules, names)
    opened = {key for key, execution in ran.items()
              if execution.get("outcome") == "QUALITY_STATUS_FAIL"}
    scenario_row_count_change(client, rep, names)
    scenario_segment(client, rep, names)
    scenario_repair(client, rep, mysql_rules, names, opened)
    specs = {spec["key"]: spec for spec in mysql_rules}
    scenario_schema_drift(client, rep, names, specs["drift-schema-drift"])
    scenario_orphan(client, rep, names, specs["orphan-row-count"])
    scenario_error_path(client, rep)
    scenario_scheduled_tick(client, rep, names)
    scenario_webhook(client, rep)
    scenario_report(client, rep)
    scenario_postgres(client, rep, names)

    return rep.summary()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("command", choices=["bootstrap", "run"])
    args = parser.parse_args()

    if not EMAIL or not PASSWORD:
        raise SystemExit("set METAXISDATA_EMAIL and METAXISDATA_PASSWORD (a workspace administrator)")
    client = Client()
    client.login()

    if args.command == "bootstrap":
        return cmd_bootstrap(client)
    return cmd_run(client)


if __name__ == "__main__":
    sys.exit(main())
