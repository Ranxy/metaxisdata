"""A DataX lineage plugin for ``sqlserverreader`` and ``sqlserverwriter``.

The DataX OpenLineage provider ships plugins for the MySQL-wire engines, PostgreSQL
and StarRocks; a reader or writer it has no plugin for is a dataset it cannot name,
and the hop then emits an event carrying no datasets at all - lineage that is not
wrong, just absent. The provider accepts extra plugins in its constructor, so this
fixture supplies its own rather than patching the provider, which lives in its own
repository.

The shapes it builds are the ones the server's resolver reads:

    namespace  ``mssql://127.0.0.1:1433``    the instance whose data source is that host and port
    name       ``e2e.e2e_ods.ads_daily_sales``  database.schema.table

The resolver splits a three-part name into database, schema and table, so that name
becomes ``ms-sql-1;e2e;e2e_ods;ads_daily_sales`` - the same GUID the schema sync
registers, which is what makes the edge land on the real object.
"""

from __future__ import annotations

import logging
import re

from openlineage.client.event_v2 import Dataset
from openlineage.client.facet_v2 import schema_dataset

from airflow_provider_datax_openlineage.lineage.plugins.base import BaseDataXLineagePlugin

log = logging.getLogger(__name__)

_HOST_PORT = re.compile(r"^//(?P<host>[^:;/]+)(?::(?P<port>\d+))?")

DEFAULT_PORT = 1433
_DATABASE_KEYS = ("databasename", "database")


def parse_jdbc_url(jdbc_url: str) -> tuple[str, int, str]:
    """Extract host, port and database from a SQL Server JDBC URL.

    Handles the spellings a DataX job can carry:

        jdbc:sqlserver://host:1433;databaseName=db
        jdbc:sqlserver://host:1433;database=db;encrypt=false
        jdbc:sqlserver://host;databaseName=db
    """
    url = jdbc_url.removeprefix("jdbc:sqlserver:")
    head, *properties = url.split(";")
    match = _HOST_PORT.match(head)
    host = match.group("host") if match else "localhost"
    port = int(match.group("port") or DEFAULT_PORT) if match else DEFAULT_PORT
    database = ""
    for prop in properties:
        key, _, value = prop.partition("=")
        if key.strip().lower() in _DATABASE_KEYS:
            database = value.strip()
    return host, port, database


def _build_schema_facet(columns: list[str]) -> schema_dataset.SchemaDatasetFacet | None:
    """Build a SchemaDatasetFacet from a DataX column list.

    Returns None if columns is empty or contains only '*' (select all).
    """
    if not columns or columns == ["*"]:
        return None
    fields = [schema_dataset.SchemaDatasetFacetFields(name=col) for col in columns]
    return schema_dataset.SchemaDatasetFacet(fields=fields)


class MssqlLineagePlugin(BaseDataXLineagePlugin):
    """Lineage plugin for sqlserverreader / sqlserverwriter."""

    def reader_names(self) -> list[str]:
        return ["sqlserverreader"]

    def writer_names(self) -> list[str]:
        return ["sqlserverwriter"]

    def parse_datasets(self, plugin_name: str, plugin_config: dict) -> list[Dataset]:
        datasets: list[Dataset] = []
        columns = plugin_config.get("column", [])
        schema_facet = _build_schema_facet(columns)
        facets = {"schema": schema_facet} if schema_facet else {}

        for conn in plugin_config.get("connection", []):
            jdbc_urls = conn.get("jdbcUrl", [])
            if isinstance(jdbc_urls, str):
                jdbc_urls = [jdbc_urls]
            tables = conn.get("table", [])
            if isinstance(tables, str):
                tables = [tables]

            for jdbc_url in jdbc_urls:
                host, port, database = parse_jdbc_url(jdbc_url)
                if not database:
                    # Without the database the name is schema.table, which the
                    # resolver reads as "<no database>;schema;table" - an object
                    # that does not exist. Say so instead of writing that edge.
                    log.warning(
                        "SQL Server JDBC URL %s names no database; the dataset cannot be "
                        "resolved to a GUID",
                        jdbc_url,
                    )
                    continue

                namespace = f"mssql://{host}:{port}"
                for table in tables:
                    datasets.append(
                        Dataset(namespace=namespace, name=f"{database}.{table}", facets=facets)
                    )

        return datasets
