<template>
  <div class="p-4 space-y-6">
    <div class="space-y-1">
      <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <div class="text-lg font-semibold wrap-break-word">
          {{ fn.name }}
        </div>
        <div
          v-if="fn.comment"
          class="text-sm text-muted-foreground wrap-break-word max-w-xl"
        >
          <ExpandableText
            :text="fn.comment"
            :item-name="fn.name"
            :dialog-title="t('metadataBrowser.comment')"
          />
        </div>
        <Button
          v-if="guid"
          variant="outline"
          size="sm"
          @click="$router.push({ path: `/explain-sql/${guid}`, query: { metaType: MetaType.FUNCTION } })"
        >
          <Sparkles class="h-3.5 w-3.5 mr-1" />
          {{ t("explainSQL.explain") }}
        </Button>
      </div>
      <div class="text-sm text-muted-foreground">
        {{ summaryLine }}
      </div>
    </div>

    <div
      v-if="fn.signature"
      class="space-y-2"
    >
      <div class="text-sm font-medium">{{ t("metadataBrowser.signature") }}</div>
      <code class="text-xs bg-muted rounded px-3 py-2 block overflow-auto whitespace-pre-wrap break-words">{{
        fn.signature
      }}</code>
    </div>

    <div class="space-y-2">
      <div class="text-sm font-medium">{{ t("metadataBrowser.definition") }}</div>
      <DefinitionMonacoViewer :content="fn.definition" />
    </div>

    <div
      v-if="fn.dependencyTables.length > 0"
      class="space-y-2"
    >
      <div class="flex items-center justify-between">
        <div class="text-sm font-medium">{{ t("metadataBrowser.dependencyTables") }}</div>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.schemaName") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.tableName") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="(dt, idx) in fn.dependencyTables"
            :key="`${dt.schema}:${dt.table}:${idx}`"
          >
            <TableCell class="font-medium">{{ dt.schema || "-" }}</TableCell>
            <TableCell class="text-muted-foreground">{{ dt.table || "-" }}</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Sparkles } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import DefinitionMonacoViewer from "@/components/metadata/DefinitionMonacoViewer.vue";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  type FunctionMetadata,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";

const props = defineProps<{ fn: FunctionMetadata; guid?: string }>();

const { t } = useI18n();

const summaryLine = computed(() => {
  const parts: string[] = [];
  if (props.fn.dependencyTables.length > 0) {
    parts.push(
      `${props.fn.dependencyTables.length} ${t("metadataBrowser.dependencyTables")}`
    );
  }
  return parts.join(" · ") || "-";
});
</script>
