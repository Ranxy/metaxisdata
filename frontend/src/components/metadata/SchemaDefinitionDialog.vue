<template>
  <Dialog v-model:open="isOpen">
    <DialogTrigger asChild>
      <slot>
        <Button
          variant="outline"
          size="sm"
        >
          <Code class="h-4 w-4 mr-2" />
          {{ t("metadataBrowser.viewSchema") }}
        </Button>
      </slot>
    </DialogTrigger>
    <DialogContent class="max-w-4xl h-[80vh] flex flex-col">
      <DialogHeader>
        <DialogTitle>{{ objectName }}</DialogTitle>
        <DialogDescription>
          {{ t("metadataBrowser.schemaDefinition") }}
        </DialogDescription>
      </DialogHeader>
      <div class="flex-1 min-h-0 rounded border overflow-hidden">
        <div
          v-if="isLoading"
          class="flex items-center justify-center h-full"
        >
          <AppLoading />
        </div>
        <div
          v-else-if="error"
          class="p-4 text-destructive"
        >
          {{ error }}
        </div>
        <MonacoEditor
          v-else
          :content="schemaContent"
          language="sql"
          readonly
          :options="editorOptions"
        />
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { Code } from "lucide-vue-next";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { getSchemaString } from "@/api/database";
import AppLoading from "@/components/common/AppLoading.vue";
import MonacoEditor from "@/components/monaco-editor/MonacoEditor.vue";
import type { IStandaloneEditorConstructionOptions } from "@/components/monaco-editor/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { extractErrorMessage } from "@/utils/error";

interface Props {
  guid: string;
  metaType: MetaType;
  objectName: string;
}

const props = defineProps<Props>();

const { t } = useI18n();

const isOpen = ref(false);
const isLoading = ref(false);
const error = ref<string | null>(null);
const schemaContent = ref("");

const editorOptions: IStandaloneEditorConstructionOptions = {
  automaticLayout: true,
  fontSize: 13,
  lineHeight: 20,
  minimap: { enabled: false },
  scrollBeyondLastLine: false,
  wordWrap: "on",
  lineNumbersMinChars: 4,
  lineDecorationsWidth: 10,
};

async function fetchSchema() {
  isLoading.value = true;
  error.value = null;
  schemaContent.value = "";

  try {
    const response = await getSchemaString({
      guid: props.guid,
      metaType: props.metaType,
    });
    schemaContent.value = response.schema;
  } catch (e) {
    error.value = extractErrorMessage(e);
  } finally {
    isLoading.value = false;
  }
}

watch(isOpen, (open) => {
  if (open) {
    fetchSchema();
  }
});
</script>
