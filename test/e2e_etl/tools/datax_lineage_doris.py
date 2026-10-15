"""A DataX lineage plugin for ``doriswriter``.

The DataX OpenLineage provider ships plugins for the MySQL-wire engines, PostgreSQL
and StarRocks; a writer it has no plugin for is a dataset it cannot name, and the
hop then emits an event carrying no datasets at all - lineage that is not wrong,
just absent. The provider accepts extra plugins in its constructor, so this fixture
supplies its own rather than patching the provider, which lives in its own
repository.

Doris is a MySQL-wire engine, so a dataset name is ``database.table`` (never
``database.schema.table``: a synced Doris object lives at
``<instance>;<database>;;<table>``, and the resolver splits a two-part name that
way for every MySQL-like engine). The shapes it builds are the ones the server's
resolver reads:

    namespace  ``doris://127.0.0.1:9031``   the instance whose data source is that host and port
    name       ``e2e_ods.dwd_customer_360`` database.table

The Doris writer addresses its target the way the StarRocks writer does — a single
``jdbcUrl`` string plus ``selectedDatabase``, rather than the URL list the readers
use. The reader this fixture pairs it with (``mysqlreader`` against the Doris FE) is
left to the provider's built-in MySQL plugin on purpose: the resolver matches a
namespace by host and port, so the Doris instance answers for it either way. That is
the fact section 25 of ``verify_chain.sql`` asserts, and section 31 forbids the
shifted GUID the naming rule can produce: the reader's URL names the database and its
table stays unqualified, because this plugin prefixes the URL's database to whatever
``table`` says. FINDINGS F10 has the measured run.
"""

from __future__ import annotations

import logging
from urllib.parse import urlparse

from openlineage.client.event_v2 import Dataset
from openlineage.client.facet_v2 import schema_dataset

from airflow_provider_datax_openlineage.lineage.plugins.base import BaseDataXLineagePlugin

log = logging.getLogger(__name__)


def parse_jdbc_url(jdbc_url: str) -> tuple[str, int, str]:
    """Extract host, port and database from a Doris (MySQL-protocol) JDBC URL.

    Handles the spellings a DataX job can carry:

        jdbc:mysql://host:9031/database
        jdbc:mysql://host:9031/database?useSSL=false
        jdbc:mysql://host/database
    """
    url = jdbc_url.removeprefix("jdbc:")
    parsed = urlparse(url)
    host = parsed.hostname or "localhost"
    port = parsed.port or 9030
    database = parsed.path.lstrip("/").split("?")[0] if parsed.path else ""
    return host, port, database


def _build_schema_facet(columns: list[str]) -> schema_dataset.SchemaDatasetFacet | None:
    """Build a SchemaDatasetFacet from a DataX column list.

    Returns None if columns is empty or contains only '*' (select all).
    """
    if not columns or columns == ["*"]:
        return None
    fields = [schema_dataset.SchemaDatasetFacetFields(name=col) for col in columns]
    return schema_dataset.SchemaDatasetFacet(fields=fields)


class DorisLineagePlugin(BaseDataXLineagePlugin):
    """Lineage plugin for doriswriter."""

    def reader_names(self) -> list[str]:
        # A Doris reader is not a DataX plugin name; the hop into Doris is fed by
        # postgresqlreader, which the provider already knows.
        return []

    def writer_names(self) -> list[str]:
        return ["doriswriter"]

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
            selected_database = conn.get("selectedDatabase", "")

            for jdbc_url in jdbc_urls:
                host, port, database = parse_jdbc_url(jdbc_url)
                if not database:
                    database = selected_database

                namespace = f"doris://{host}:{port}"
                for table in tables:
                    name = f"{database}.{table}" if database else table
                    datasets.append(Dataset(namespace=namespace, name=name, facets=facets))

        if not datasets:
            log.warning(
                "No Doris datasets extracted from %s config; a jdbcUrl is required",
                plugin_name,
            )

        return datasets
