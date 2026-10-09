// Monaco 0.57 ships no declarations beside the SQL language definition, so type
// its two exports from the public API. The editor entry needs no shim: its
// `editor.api.d.ts` sits next to `editor.api.js`, which bundler resolution finds
// through the package's `exports` map.
declare module "monaco-editor/languages/definitions/sql/sql.js" {
  import type * as monaco from "monaco-editor";
  export const conf: monaco.languages.LanguageConfiguration;
  export const language: monaco.languages.IMonarchLanguage;
}
