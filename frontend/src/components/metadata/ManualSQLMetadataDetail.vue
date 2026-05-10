<template>
  <div class="p-4 space-y-6">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div class="text-lg font-semibold wrap-break-word">
          {{ displayTitle }}
        </div>
        <div
          v-if="manualSql.comment"
          class="text-sm text-muted-foreground wrap-break-word max-w-xl"
        >
          <ExpandableText
            :text="manualSql.comment"
            :item-name="displayTitle"
            :dialog-title="t('metadataBrowser.comment')"
          />
        </div>
      </div>
      <div
        v-if="showResourceName"
        class="text-sm text-muted-foreground wrap-break-word"
      >
        {{ manualSql.name }}
      </div>
      <div class="text-sm text-muted-foreground">
        {{ summaryLine }}
      </div>
    </div>

    <div class="space-y-2">
      <div class="text-sm font-medium">{{ t("metadataBrowser.tableInfo") }}</div>
      <div class="flex flex-wrap gap-2">
        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.databaseName") }}</div>
          <div class="text-sm font-medium wrap-break-word">{{ manualSql.databaseName || "-" }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.schemaName") }}</div>
          <div class="text-sm font-medium wrap-break-word">{{ schemaDisplayName }}</div>
        </div>

        <div
          v-if="manualSql.tags.length > 0"
          class="min-w-[16rem] rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.tags") }}</div>
          <div class="mt-2 flex flex-wrap gap-1.5">
            <Badge
              v-for="tag in manualSql.tags"
              :key="tag"
              variant="secondary"
            >
              {{ tag }}
            </Badge>
          </div>
        </div>

        <div
          v-if="manualSql.comment"
          class="min-w-[18rem] max-w-2xl rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.comment") }}</div>
          <div class="mt-1 text-sm text-foreground wrap-break-word">
            <ExpandableText
              :text="manualSql.comment"
              :item-name="displayTitle"
              :dialog-title="t('metadataBrowser.comment')"
            />
          </div>
        </div>

        <div
          v-for="attribute in attributeEntries"
          :key="attribute.key"
          class="min-w-[13rem] rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ attribute.key }}</div>
          <div class="text-sm font-medium break-all">{{ attribute.value }}</div>
        </div>
      </div>
    </div>

    <div class="space-y-2">
      <div class="text-sm font-medium">{{ t("metadataBrowser.sqlText") }}</div>
      <DefinitionMonacoViewer
        :content="manualSql.sqlText"
        :min-height="180"
        :max-height="640"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import type { ManualSQLMetadata } from "@/types/proto-es/v1/database_service_pb";
import DefinitionMonacoViewer from "./DefinitionMonacoViewer.vue";
import ExpandableText from "./ExpandableText.vue";

const props = defineProps<{
  manualSql: ManualSQLMetadata;
}>();

const { t } = useI18n();

const displayTitle = computed(
  () => props.manualSql.title || props.manualSql.name
);

const showResourceName = computed(
  () => props.manualSql.title && props.manualSql.title !== props.manualSql.name
);

const schemaDisplayName = computed(
  () => props.manualSql.schemaName || t("metadataBrowser.defaultSchema")
);

const attributeEntries = computed(() => {
  return Object.entries(props.manualSql.attributes ?? {}).map(
    ([key, value]) => ({
      key,
      value,
    })
  );
});

const summaryLine = computed(() => {
  const parts = [
    `${t("metadataBrowser.databaseName")}: ${props.manualSql.databaseName || "-"}`,
    `${t("metadataBrowser.schemaName")}: ${schemaDisplayName.value}`,
  ];

  if (props.manualSql.tags.length > 0) {
    parts.push(`${props.manualSql.tags.length} ${t("metadataBrowser.tags")}`);
  }

  if (attributeEntries.value.length > 0) {
    parts.push(
      `${attributeEntries.value.length} ${t("metadataBrowser.attributes")}`
    );
  }

  return parts.join(" · ");
});
</script>