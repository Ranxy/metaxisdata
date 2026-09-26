// Monaco 0.55 ships these ESM entry points without types that TypeScript's
// bundler resolution can follow (its `./*` export targets have no extension),
// so point them at the package's public declarations.
declare module "monaco-editor/esm/vs/editor/editor.api" {
  export * from "monaco-editor";
}

declare module "monaco-editor/esm/vs/basic-languages/sql/sql.js" {
  import type * as monaco from "monaco-editor";
  export const conf: monaco.languages.LanguageConfiguration;
  export const language: monaco.languages.IMonarchLanguage;
}
