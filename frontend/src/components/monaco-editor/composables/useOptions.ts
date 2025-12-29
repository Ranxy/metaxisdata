import type * as monaco from "monaco-editor";
import type { Ref } from "vue";
import { watch } from "vue";
import type {
  IStandaloneEditorConstructionOptions,
  MonacoModule,
} from "../types";

export function useOptions(
  _monaco: MonacoModule,
  editor: monaco.editor.IStandaloneCodeEditor,
  options: Ref<IStandaloneEditorConstructionOptions | undefined>
) {
  watch(
    options,
    (opts) => {
      if (opts) {
        editor.updateOptions(opts);
      }
    },
    { immediate: true, deep: true }
  );
}

export function useReadonly(
  _monaco: MonacoModule,
  editor: monaco.editor.IStandaloneCodeEditor,
  readonly: Ref<boolean>
) {
  watch(
    readonly,
    (value) => {
      editor.updateOptions({ readOnly: value });
    },
    { immediate: true }
  );
}
