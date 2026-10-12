<template>
  <div class="p-4 space-y-4">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div class="text-lg font-semibold wrap-break-word">
          {{ displayTitle }}
        </div>
        <Button
          v-if="guid"
          variant="outline"
          size="sm"
          @click="openStatement"
        >
          <TerminalSquare class="h-3.5 w-3.5 mr-1" />
          {{ t("applicationSql.viewStatement") }}
        </Button>
        <Button
          v-if="guid"
          variant="outline"
          size="sm"
          @click="openExplainSQL"
        >
          <Sparkles class="h-3.5 w-3.5 mr-1" />
          {{ t("explainSQL.explain") }}
        </Button>
      </div>
      <div class="text-sm text-muted-foreground wrap-break-word">
        {{ summaryLine }}
      </div>
    </div>

    <MetadataTabGroup
      v-if="guid"
      v-model="activeTab"
      :tabs="tabs"
    />

    <template v-if="!guid || activeTab === 'details'">
      <div class="space-y-2">
        <h2 class="text-sm font-medium">
          {{ t("metadataBrowser.tableInfo") }}
        </h2>
        <div class="flex flex-wrap gap-2">
          <div class="rounded-md border px-3 py-2">
            <div class="text-xs text-muted-foreground">
              {{ t("metadataBrowser.databaseName") }}
            </div>
            <div class="text-sm font-medium wrap-break-word">
              {{ applicationSql.databaseName || "-" }}
            </div>
          </div>

          <div class="rounded-md border px-3 py-2">
            <div class="text-xs text-muted-foreground">
              {{ t("metadataBrowser.schemaName") }}
            </div>
            <div class="text-sm font-medium wrap-break-word">
              {{ schemaDisplayName }}
            </div>
          </div>

          <div class="rounded-md border px-3 py-2">
            <div class="text-xs text-muted-foreground">
              {{ t("metadataBrowser.instance") }}
            </div>
            <div class="text-sm font-medium wrap-break-word">
              {{ applicationSql.instanceResource || "-" }}
            </div>
          </div>
        </div>
      </div>

      <div class="space-y-2">
        <h2 class="text-sm font-medium">
          {{ t("metadataBrowser.definition") }}
        </h2>
        <pre
          class="overflow-x-auto rounded-md border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap"
        >{{ applicationSql.sqlText }}</pre>
        <p class="text-xs text-muted-foreground">
          {{ t("applicationSql.detailHint") }}
        </p>
      </div>

      <TableLineageSection
        v-if="guid"
        :guid="guid"
        :meta-type="MetaType.APPLICATION_SQL"
        :title="t('metadataBrowser.relatedLineageAnalysis')"
      />
    </template>

    <MetadataHistorySection
      v-else
      :guid="guid"
      :meta-type="MetaType.APPLICATION_SQL"
    />
  </div>
</template>

<script setup lang="ts">
import { Sparkles, TerminalSquare } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import MetadataHistorySection from "@/components/metadata/MetadataHistorySection.vue";
import MetadataTabGroup from "@/components/metadata/MetadataTabGroup.vue";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import { Button } from "@/components/ui/button";
import {
  type ApplicationSQLMetadata,
  MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import { guidToRouteParams } from "@/utils/guid";

const props = defineProps<{
  applicationSql: ApplicationSQLMetadata;
  guid?: string;
}>();

const { t } = useI18n();
const router = useRouter();

const activeTab = ref<"details" | "history">("details");

const tabs = computed(() => [
  {
    value: "details" as const,
    label: t("metadataBrowser.applicationSqlDetail"),
  },
  { value: "history" as const, label: t("metadataBrowser.historyTitle") },
]);

const displayTitle = computed(() => props.applicationSql.name);

const schemaDisplayName = computed(
  () => props.applicationSql.schemaName || t("metadataBrowser.defaultSchema")
);

const summaryLine = computed(() =>
  [
    `${t("metadataBrowser.databaseName")}: ${props.applicationSql.databaseName || "-"}`,
    `${t("metadataBrowser.schemaName")}: ${schemaDisplayName.value}`,
  ].join(" · ")
);

// Counts, kind and the applications that reported it live on the statement's own page:
// the registry row is written once, so it does not carry numbers that change per report.
function openStatement() {
  if (!props.guid) {
    return;
  }
  void router.push({ name: "ApplicationSQL", query: { guid: props.guid } });
}

function openExplainSQL() {
  if (!props.guid) {
    return;
  }
  void router.push({
    name: "ExplainSQLWithGuid",
    params: { guid: guidToRouteParams(props.guid) },
    query: { metaType: MetaType.APPLICATION_SQL },
  });
}
</script>
