<template>
	<div ref="containerRef" class="monaco-editor-container">
		<div
			v-if="!ready"
			class="absolute inset-0 flex items-center justify-center"
		>
			<span class="text-gray-500">Loading editor...</span>
		</div>
	</div>
</template>

<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  toRef,
  watch,
} from "vue";
import {
  useContent,
  useFormatContent,
  useOptions,
  useReadonly,
} from "./composables";
import { createMonacoEditor } from "./editor";
import type {
  IStandaloneCodeEditor,
  IStandaloneEditorConstructionOptions,
  Language,
  MonacoModule,
  SQLDialect,
} from "./types";

interface Props {
  content: string;
  language?: Language;
  readonly?: boolean;
  options?: IStandaloneEditorConstructionOptions;
  sqlDialect?: SQLDialect;
}

interface Emits {
  (event: "update:content", content: string): void;
  (event: "ready", monaco: MonacoModule, editor: IStandaloneCodeEditor): void;
}

const props = withDefaults(defineProps<Props>(), {
  language: "plaintext",
  readonly: false,
  options: undefined,
});

const emit = defineEmits<Emits>();

const containerRef = ref<HTMLDivElement>();
const editorRef = shallowRef<IStandaloneCodeEditor>();
const monacoRef = shallowRef<MonacoModule>();
const ready = ref(false);

const contentValue = computed({
  get() {
    return props.content;
  },
  set(value) {
    emit("update:content", value);
  },
});

onMounted(async () => {
  const container = containerRef.value;
  if (!container) {
    console.debug(
      "<MonacoEditor> has been unmounted before monaco-editor initialized"
    );
    return;
  }

  try {
    const { editor, monaco } = await createMonacoEditor({
      container,
      options: {
        value: props.content,
        language: props.language,
        readOnly: props.readonly,
        ...props.options,
      },
    });

    editorRef.value = editor;
    monacoRef.value = monaco;

    useReadonly(monaco, editor, toRef(props, "readonly"));
    useOptions(monaco, editor, toRef(props, "options"));
    useFormatContent(monaco, editor, toRef(props, "sqlDialect"));

    const content = useContent(monaco, editor);

    ready.value = true;

    await nextTick();
    emit("ready", monaco, editor);

    watch(content, () => {
      emit("update:content", content.value);
    });

    watch(
      () => contentValue.value,
      (newContent) => {
        const model = editor.getModel();
        if (model && model.getValue() !== newContent) {
          model.setValue(newContent);
        }
      }
    );

    watch(
      () => props.language,
      (newLanguage) => {
        const model = editor.getModel();
        if (model && monacoRef.value) {
          monacoRef.value.editor.setModelLanguage(model, newLanguage);
        }
      }
    );
  } catch (ex) {
    console.error("[MonacoEditor] initialize failed", ex);
  }
});

onBeforeUnmount(() => {
  editorRef.value?.dispose();
});

defineExpose({
  get editor() {
    return editorRef.value;
  },
});
</script>

<style scoped>
.monaco-editor-container {
	position: relative;
	width: 100%;
	height: 100%;
  min-height: 0;
}

.monaco-editor-container :deep(.monaco-editor) {
	outline: none !important;
}

.monaco-editor-container :deep(.monaco-editor .scroll-decoration) {
	display: none !important;
}
</style>
