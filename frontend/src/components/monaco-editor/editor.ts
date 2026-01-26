import type * as MonacoType from "monaco-editor";
import { loadMonacoEditor } from "./lazy-editor";

export interface CreateMonacoEditorResult {
  editor: MonacoType.editor.IStandaloneCodeEditor;
  monaco: typeof MonacoType;
}

export async function createMonacoEditor(config: {
  container: HTMLElement;
  options?: MonacoType.editor.IStandaloneEditorConstructionOptions;
}): Promise<CreateMonacoEditorResult> {
  const monaco = await loadMonacoEditor();

  const defaults = defaultEditorOptions();
  const editor = monaco.editor.create(config.container, {
    ...defaults,
    ...config.options,
    // Deep merge scrollbar options to ensure alwaysConsumeMouseWheel is preserved
    scrollbar: {
      ...defaults.scrollbar,
      ...config.options?.scrollbar,
      // Always allow scroll events to propagate when at boundaries
      alwaysConsumeMouseWheel: false,
    },
  });

  return { editor, monaco };
}

export function defaultEditorOptions(): MonacoType.editor.IStandaloneEditorConstructionOptions {
  return {
    theme: "vs",
    tabSize: 2,
    insertSpaces: true,
    autoClosingQuotes: "never",
    detectIndentation: false,
    folding: true,
    automaticLayout: true,
    minimap: {
      enabled: false,
    },
    wordWrap: "on",
    fixedOverflowWidgets: true,
    fontSize: 14,
    lineHeight: 24,
    scrollBeyondLastLine: false,
    padding: {
      top: 8,
      bottom: 8,
    },
    renderLineHighlight: "line",
    scrollbar: {
      alwaysConsumeMouseWheel: false,
    },
    lineNumbers: "on",
    cursorStyle: "line",
    glyphMargin: false,
  };
}
