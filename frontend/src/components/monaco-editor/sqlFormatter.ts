import type { FormatOptionsWithLanguage } from "sql-formatter";
import type { SQLDialect } from "./types";

export interface FormatResult {
  data: string;
  error: Error | null;
}

type FormatterLanguage = FormatOptionsWithLanguage["language"];

function convertDialectToFormatterLanguage(
  dialect: SQLDialect | undefined
): FormatterLanguage {
  if (dialect === "mysql" || dialect === "tidb") return "mysql";
  if (dialect === "postgresql" || dialect === "postgres") return "postgresql";
  if (dialect === "snowflake") return "snowflake";
  if (dialect === "sqlite") return "sqlite";
  if (dialect === "mariadb") return "mariadb";
  if (dialect === "bigquery") return "bigquery";
  if (dialect === "db2") return "db2";
  if (dialect === "hive") return "hive";
  if (dialect === "n1ql") return "n1ql";
  if (dialect === "plsql" || dialect === "oracle") return "plsql";
  if (dialect === "redshift") return "redshift";
  if (dialect === "spark") return "spark";
  if (dialect === "trino") return "trino";
  if (dialect === "transactsql" || dialect === "tsql" || dialect === "mssql")
    return "transactsql";
  if (dialect === "singlestoredb") return "singlestoredb";
  return "sql";
}

export async function formatSQL(
  sql: string,
  dialect: SQLDialect | undefined
): Promise<FormatResult> {
  const { format } = await import("sql-formatter");
  const options: Partial<FormatOptionsWithLanguage> = {
    language: convertDialectToFormatterLanguage(dialect),
  };

  try {
    const formatted = format(sql, options);
    return { data: formatted, error: null };
  } catch (error) {
    return { data: "", error: error as Error };
  }
}
