<template>
  <div class="p-4 space-y-6">
    <div class="space-y-1">
      <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <div class="text-lg font-semibold wrap-break-word">
          {{ proc.name }}
        </div>
        <div
          v-if="proc.comment"
          class="text-sm text-muted-foreground wrap-break-word max-w-xl"
        >
          <ExpandableText
            :text="proc.comment"
            :item-name="proc.name"
            :dialog-title="t('metadataBrowser.comment')"
          />
        </div>
        <Button
          v-if="guid"
          variant="outline"
          size="sm"
          @click="$router.push(`/explain-sql/${guid}`)"
        >
          <Sparkles class="h-3.5 w-3.5 mr-1" />
          {{ t("explainSQL.explain") }}
        </Button>
      </div>
      <div
        v-if="proc.signature"
        class="text-sm text-muted-foreground"
      >
        {{ proc.signature }}
      </div>
    </div>

    <div
      v-if="proc.signature"
      class="space-y-2"
    >
      <div class="text-sm font-medium">{{ t("metadataBrowser.signature") }}</div>
      <code class="text-xs bg-muted rounded px-3 py-2 block overflow-auto whitespace-pre-wrap break-words">{{
        proc.signature
      }}</code>
    </div>

    <div class="space-y-2">
      <div class="text-sm font-medium">{{ t("metadataBrowser.definition") }}</div>
      <DefinitionMonacoViewer :content="proc.definition" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import { Sparkles } from "lucide-vue-next";
import DefinitionMonacoViewer from "@/components/metadata/DefinitionMonacoViewer.vue";
import type { ProcedureMetadata } from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";

defineProps<{ proc: ProcedureMetadata; guid?: string }>();

const { t } = useI18n();
</script>
