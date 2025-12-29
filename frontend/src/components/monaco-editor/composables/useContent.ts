import type * as monaco from "monaco-editor";
import { ref } from "vue";
import type { MonacoModule } from "../types";

export function useContent(
  _monaco: MonacoModule,
  editor: monaco.editor.IStandaloneCodeEditor
) {
  const content = ref(getContent(editor));

  const update = () => {
    content.value = getContent(editor);
  };

  editor.onDidChangeModel(update);
  editor.onDidChangeModelContent(update);

  return content;
}

function getContent(editor: monaco.editor.IStandaloneCodeEditor) {
  const model = editor.getModel();
  if (!model) return "";
  return model.getValue();
}
