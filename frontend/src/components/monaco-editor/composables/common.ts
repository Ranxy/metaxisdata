import { ref } from "vue";
// Type-only on purpose: a value import of `../monaco` here would make the whole
// editor bundle a static dependency of every consumer of these composables —
// see the note in `lazy-editor.ts`. The module is passed in at run time instead.
import type * as monaco from "../monaco";
import { formatSQL } from "../sqlFormatter";
import type { MonacoModule, SQLDialect } from "../types";

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
  monaco: MonacoModule,
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

  trySetContentWithUndo(monaco, editor, model, data, "Format SQL");
}

export function trySetContentWithUndo(
  monaco: MonacoModule,
  editor: monaco.editor.IStandaloneCodeEditor,
  model: monaco.editor.ITextModel,
  content: string,
  source?: string
) {
  const lineCount = model.getLineCount();
  const lastLineLength = model.getLineLength(lineCount);

  editor.executeEdits(source, [
    {
      range: new monaco.Range(1, 1, lineCount, lastLineLength + 1),
      text: content,
      forceMoveMarkers: true,
    },
  ]);

  editor.setPosition({ lineNumber: 1, column: 1 });
}
