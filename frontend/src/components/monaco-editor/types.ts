import type * as monaco from "monaco-editor";

export type MonacoModule = typeof monaco;

export type IStandaloneCodeEditor = monaco.editor.IStandaloneCodeEditor;
export type IStandaloneEditorConstructionOptions =
  monaco.editor.IStandaloneEditorConstructionOptions;

export type ITextModel = monaco.editor.ITextModel;

export type Language = "sql" | "javascript" | "json" | "plaintext";

export type SQLDialect =
  | "mysql"
  | "postgresql"
  | "postgres"
  | "sqlite"
  | "mariadb"
  | "bigquery"
  | "db2"
  | "hive"
  | "n1ql"
  | "plsql"
  | "oracle"
  | "redshift"
  | "spark"
  | "trino"
  | "transactsql"
  | "tsql"
  | "mssql"
  | "singlestoredb"
  | "snowflake"
  | "tidb";

export type Selection = monaco.Selection;

export interface MonacoEditorProps {
  content: string;
  language?: Language;
  readonly?: boolean;
  options?: IStandaloneEditorConstructionOptions;
}

export interface MonacoEditorEmits {
  (event: "update:content", content: string): void;
  (event: "ready", monaco: MonacoModule, editor: IStandaloneCodeEditor): void;
}
