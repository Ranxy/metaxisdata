import type * as monaco from "monaco-editor";
import type { Ref } from "vue";
import { unref, watchEffect } from "vue";
import type { MonacoModule, SQLDialect } from "../types";
import { formatEditorContent, useTextModelLanguage } from "./common";

export interface FormatContentOptions {
  disabled?: boolean;
  callback?: (
    monaco: MonacoModule,
    editor: monaco.editor.IStandaloneCodeEditor
  ) => void;
}

const defaultOptions = (): FormatContentOptions => ({
  disabled: false,
});

export function useFormatContent(
  monaco: MonacoModule,
  editor: monaco.editor.IStandaloneCodeEditor,
  dialect: Ref<SQLDialect | undefined>,
  options?: Ref<FormatContentOptions | undefined>
) {
  const language = useTextModelLanguage(editor);
  let action: monaco.IDisposable | undefined;

  watchEffect(() => {
    const opts = {
      ...defaultOptions(),
      ...unref(options),
    };

    if (action) {
      action.dispose();
      action = undefined;
    }

    if (opts.disabled) return;

    if (language.value === "sql") {
      action = editor.addAction({
        id: "format-sql",
        label: "Format SQL",
        keybindings: [
          monaco.KeyMod.Alt | monaco.KeyMod.Shift | monaco.KeyCode.KeyF,
        ],
        contextMenuGroupId: "operation",
        contextMenuOrder: 1,
        run: async () => {
          if (opts.callback) {
            opts.callback(monaco, editor);
            return;
          }
          const readonly = editor.getOption(
            monaco.editor.EditorOption.readOnly
          );
          if (readonly) return;
          await formatEditorContent(editor, dialect.value);
        },
      });
    }
  });
}
