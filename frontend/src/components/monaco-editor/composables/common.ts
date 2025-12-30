import type * as monaco from "monaco-editor";
import { Range } from "monaco-editor";
import { ref } from "vue";
import { formatSQL } from "../sqlFormatter";
import type { SQLDialect } from "../types";

export function useTextModelLanguage(
  editor: monaco.editor.IStandaloneCodeEditor
) {
  const language = ref(getModelLanguage(editor));

  const update = () => {
    language.value = getModelLanguage(editor);
  };

  editor.onDidChangeModel(update);

  const model = editor.getModel();
  if (model) {
    model.onDidChangeLanguage(update);
  }

  return language;
}

function getModelLanguage(editor: monaco.editor.IStandaloneCodeEditor): string {
  const model = editor.getModel();
  if (!model) return "";
  return model.getLanguageId();
}

export async function formatEditorContent(
  editor: monaco.editor.IStandaloneCodeEditor,
  dialect: SQLDialect | undefined
) {
  const model = editor.getModel();
  if (!model) return;

  const sql = model.getValue();
  const { data, error } = await formatSQL(sql, dialect);

  if (error) {
    console.error("[formatEditorContent] Format error:", error);
    return;
  }

  trySetContentWithUndo(editor, model, data, "Format SQL");
}

export function trySetContentWithUndo(
  editor: monaco.editor.IStandaloneCodeEditor,
  model: monaco.editor.ITextModel,
  content: string,
  source?: string
) {
  const lineCount = model.getLineCount();
  const lastLineLength = model.getLineLength(lineCount);

  editor.executeEdits(source, [
    {
      range: new Range(1, 1, lineCount, lastLineLength + 1),
      text: content,
      forceMoveMarkers: true,
    },
  ]);

  editor.setPosition({ lineNumber: 1, column: 1 });
}
