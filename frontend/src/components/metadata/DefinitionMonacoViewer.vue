<template>
  <div
    v-if="normalizedContent"
    class="rounded border overflow-hidden"
    :style="{ height: `${editorHeight}px` }"
  >
    <MonacoEditor
      :content="normalizedContent"
      :language="language"
      readonly
      :options="mergedOptions"
      @ready="handleEditorReady"
    />
  </div>

  <pre
    v-else
    class="text-xs bg-muted rounded p-3 overflow-auto whitespace-pre-wrap break-words"
  >{{ emptyText }}</pre>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from "vue";
import MonacoEditor from "@/components/monaco-editor/MonacoEditor.vue";
import type {
  IStandaloneCodeEditor,
  IStandaloneEditorConstructionOptions,
  Language,
  MonacoModule,
} from "@/components/monaco-editor/types";

interface Props {
  content?: string;
  language?: Language;
  emptyText?: string;
  minHeight?: number;
  maxHeight?: number;
  options?: IStandaloneEditorConstructionOptions;
}

const props = withDefaults(defineProps<Props>(), {
  content: "",
  language: "sql",
  emptyText: "-",
  minHeight: 120,
  maxHeight: 520,
  options: undefined,
});

const editorHeight = ref(180);

let contentSizeDispose: { dispose: () => void } | undefined;

const normalizedContent = computed(() => {
  const value = (props.content ?? "").trimEnd();
  return value ? value : "";
});

const baseOptions: IStandaloneEditorConstructionOptions = {
  automaticLayout: true,
  fontSize: 12,
  lineHeight: 18,
  padding: {
    top: 6,
    bottom: 6,
  },
  minimap: { enabled: false },
  folding: false,
  scrollBeyondLastLine: false,
  wordWrap: "on",
  lineNumbersMinChars: 2,
  lineDecorationsWidth: 0,
  glyphMargin: false,
  overviewRulerLanes: 0,
  renderLineHighlight: "none",
  tabSize: 2,
  scrollbar: {
    verticalScrollbarSize: 10,
    horizontalScrollbarSize: 10,
  },
};

const mergedOptions = computed<IStandaloneEditorConstructionOptions>(() => {
  return {
    ...baseOptions,
    ...props.options,
  };
});

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

function updateEditorHeight(editor: IStandaloneCodeEditor) {
  const contentHeight = editor.getContentHeight();
  const target = clamp(
    Math.ceil(contentHeight) + 2,
    props.minHeight,
    props.maxHeight
  );
  if (editorHeight.value !== target) {
    editorHeight.value = target;
  }
}

function handleEditorReady(
  _monaco: MonacoModule,
  editor: IStandaloneCodeEditor
) {
  updateEditorHeight(editor);

  contentSizeDispose?.dispose();
  contentSizeDispose = editor.onDidContentSizeChange(() => {
    updateEditorHeight(editor);
  });
}

onBeforeUnmount(() => {
  contentSizeDispose?.dispose();
});
</script>
