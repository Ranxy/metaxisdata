<template>
  <div class="space-y-4 p-4">
    <!-- Identity and actions. The entity name is the page title: the surrounding
         page no longer repeats it in a heading or a breadcrumb card. -->
    <div class="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
      <div class="min-w-0 space-y-1">
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h1 class="truncate text-xl font-semibold tracking-tight">
            {{ table.name }}
          </h1>
          <Badge
            variant="secondary"
            class="shrink-0"
          >
            {{ t("metadataBrowser.tables") }}
          </Badge>
          <div
            v-if="table.userComment || table.comment"
            class="min-w-0 max-w-xl text-sm text-muted-foreground"
          >
            <ExpandableText
              :text="table.userComment || table.comment"
              :item-name="table.name"
              :dialog-title="t('metadataBrowser.comment')"
            />
          </div>
        </div>

        <!-- Table Info used to occupy a full section of bordered chips; the
             values now sit on one muted line under the title. -->
        <p class="text-sm text-muted-foreground">
          {{ metaLine }}
        </p>
      </div>

      <div class="flex shrink-0 flex-wrap items-center gap-2">
        <SchemaDefinitionDialog
          v-if="guid"
          :guid="guid"
          :meta-type="MetaType.TABLE"
          :object-name="table.name"
        />
        <Button
          v-if="guid"
          variant="outline"
          size="sm"
          @click="$router.push({ name: 'ExplainSQLWithGuid', params: { guid: guidToRouteParams(guid ?? '') }, query: { metaType: MetaType.TABLE } })"
        >
          <Sparkles class="mr-1 h-3.5 w-3.5" />
          {{ t("explainSQL.explain") }}
        </Button>
      </div>
    </div>

    <!-- One tab row for the whole detail view. History used to stack a second
         tab row on top of this one. -->
    <MetadataTabGroup
      v-model="activeTab"
      :tabs="tabs"
    />

    <template v-if="activeTab === 'columns'">
      <MetadataColumnsSection
        :columns="table.columns"
        :selected-column-name="selectedColumnName"
        :row-ref="setColumnRowRef"
      />
    </template>

    <template v-else-if="activeTab === 'indexes'">
      <Table v-if="table.indexes.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.indexName") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.indexType") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.expressions") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.unique") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.primary") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="idx in table.indexes"
            :key="idx.name"
          >
            <TableCell class="font-medium">{{ idx.name }}</TableCell>
            <TableCell class="text-muted-foreground">{{ idx.type || "-" }}</TableCell>
            <TableCell class="max-w-md truncate text-muted-foreground">
              {{ idx.expressions.join(", ") || "-" }}
            </TableCell>
            <TableCell>
              <Badge :variant="idx.unique ? 'success' : 'secondary'">
                {{ idx.unique ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
              </Badge>
            </TableCell>
            <TableCell>
              <Badge :variant="idx.primary ? 'success' : 'secondary'">
                {{ idx.primary ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
              </Badge>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>

      <div
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ t("metadataBrowser.noIndexes") }}
      </div>
    </template>

    <TableLineageSection
      v-else-if="activeTab === 'lineage' && guid"
      :guid="guid"
      :meta-type="MetaType.TABLE"
      :title="t('metadataBrowser.relatedLineageAnalysis')"
    />

    <Table
      v-else-if="activeTab === 'foreignKeys'"
    >
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.foreignKeyName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.columns") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.references") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.onDelete") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.onUpdate") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="fk in table.foreignKeys"
          :key="fk.name"
        >
          <TableCell class="font-medium">{{ fk.name }}</TableCell>
          <TableCell class="text-muted-foreground">{{ fk.columns.join(", ") }}</TableCell>
          <TableCell class="text-muted-foreground">
            {{ fk.referencedSchema ? `${fk.referencedSchema}.` : "" }}{{ fk.referencedTable }}
            ({{ fk.referencedColumns.join(", ") }})
          </TableCell>
          <TableCell class="text-muted-foreground">{{ fk.onDelete || "-" }}</TableCell>
          <TableCell class="text-muted-foreground">{{ fk.onUpdate || "-" }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <Table v-else-if="activeTab === 'checkConstraints'">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.constraintName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.expression") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="cc in table.checkConstraints"
          :key="cc.name"
        >
          <TableCell class="font-medium">{{ cc.name }}</TableCell>
          <TableCell class="max-w-xl truncate text-muted-foreground">{{ cc.expression }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <Table v-else-if="activeTab === 'partitions'">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.partitionName") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.partitionType") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.expression") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.value") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="p in table.partitions"
          :key="p.name"
        >
          <TableCell class="font-medium">{{ p.name }}</TableCell>
          <TableCell class="text-muted-foreground">{{ String(p.type) }}</TableCell>
          <TableCell class="max-w-xl truncate text-muted-foreground">{{ p.expression || "-" }}</TableCell>
          <TableCell class="text-muted-foreground">{{ p.value || "-" }}</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <MetadataHistorySection
      v-else-if="activeTab === 'history' && guid"
      :guid="guid"
      :meta-type="MetaType.TABLE"
    />
  </div>
</template>

<script setup lang="ts">
import { Sparkles } from "lucide-vue-next";
import {
  type ComponentPublicInstance,
  computed,
  nextTick,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import MetadataHistorySection from "@/components/metadata/MetadataHistorySection.vue";
import TableLineageSection from "@/components/metadata/TableLineageSection.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Engine } from "@/types/proto-es/v1/common_pb";
import {
  MetaType,
  type TableMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import { formatBytes, formatNumber } from "@/utils/format";
import { guidToRouteParams } from "@/utils/guid";
import ExpandableText from "./ExpandableText.vue";
import MetadataColumnsSection from "./MetadataColumnsSection.vue";
import MetadataTabGroup from "./MetadataTabGroup.vue";
import SchemaDefinitionDialog from "./SchemaDefinitionDialog.vue";

const props = defineProps<{
  table: TableMetadata;
  instanceEngine?: Engine | null;
  guid?: string;
  selectedColumnName?: string;
}>();

const { t } = useI18n();

type DetailTab =
  | "columns"
  | "indexes"
  | "lineage"
  | "foreignKeys"
  | "checkConstraints"
  | "partitions"
  | "history";

const activeTab = ref<DetailTab>("columns");
const columnRowRefs = new Map<string, Element>();

const selectedColumnName = computed(
  () => props.selectedColumnName?.trim() || ""
);

// Columns stay the landing tab: it is the content a user opens a table for.
// Indexes, lineage and history used to be stacked below it, which pushed the
// first column row past three quarters of the viewport.
const tabs = computed(() => {
  const list: Array<{ value: DetailTab; label: string; count?: number }> = [
    {
      value: "columns",
      label: t("metadataBrowser.columns"),
      count: props.table.columns.length,
    },
    {
      value: "indexes",
      label: t("metadataBrowser.indexes"),
      count: props.table.indexes.length,
    },
  ];
  if (props.guid) {
    list.push({ value: "lineage", label: t("metadataBrowser.lineage") });
  }
  if (props.table.foreignKeys.length > 0) {
    list.push({
      value: "foreignKeys",
      label: t("metadataBrowser.foreignKeys"),
      count: props.table.foreignKeys.length,
    });
  }
  if (props.table.checkConstraints.length > 0) {
    list.push({
      value: "checkConstraints",
      label: t("metadataBrowser.checkConstraints"),
      count: props.table.checkConstraints.length,
    });
  }
  if (props.table.partitions.length > 0) {
    list.push({
      value: "partitions",
      label: t("metadataBrowser.partitions"),
      count: props.table.partitions.length,
    });
  }
  if (props.guid) {
    list.push({ value: "history", label: t("metadataBrowser.historyTitle") });
  }
  return list;
});

function setColumnRowRef(
  columnName: string,
  target: Element | ComponentPublicInstance | null
) {
  const element =
    target instanceof Element
      ? target
      : target?.$el instanceof Element
        ? target.$el
        : null;
  if (!element) {
    columnRowRefs.delete(columnName);
    return;
  }
  columnRowRefs.set(columnName, element);
}

async function focusSelectedColumn() {
  if (!selectedColumnName.value) {
    return;
  }

  await nextTick();
  columnRowRefs.get(selectedColumnName.value)?.scrollIntoView({
    behavior: "smooth",
    block: "center",
  });
}

watch(
  selectedColumnName,
  async (value) => {
    if (!value) {
      return;
    }
    activeTab.value = "columns";
    await focusSelectedColumn();
  },
  { immediate: true }
);

const isMySQLFamily = computed(() => {
  if (!props.instanceEngine) return false;
  return (
    props.instanceEngine === Engine.MYSQL ||
    props.instanceEngine === Engine.MARIADB ||
    props.instanceEngine === Engine.TIDB ||
    props.instanceEngine === Engine.STARROCKS ||
    props.instanceEngine === Engine.DORIS
  );
});

const isPostgres = computed(() => props.instanceEngine === Engine.POSTGRES);

// One muted line instead of a section of bordered chips. Counts lead because
// they are the values a reader scans for.
const metaLine = computed(() => {
  const parts: string[] = [];
  if (props.table.columns.length > 0) {
    parts.push(`${props.table.columns.length} ${t("metadataBrowser.columns")}`);
  }
  if (props.table.indexes.length > 0) {
    parts.push(`${props.table.indexes.length} ${t("metadataBrowser.indexes")}`);
  }

  const facts: Array<[string, string]> = [];
  if (isMySQLFamily.value) {
    facts.push([t("metadataBrowser.engine"), props.table.engine || "-"]);
  }
  if (isPostgres.value) {
    facts.push([t("metadataBrowser.owner"), props.table.owner || "-"]);
  }
  facts.push([
    t("metadataBrowser.rowCount"),
    formatNumber(props.table.rowCount),
  ]);
  facts.push([
    t("metadataBrowser.dataSize"),
    formatBytes(props.table.dataSize),
  ]);
  if (props.table.indexSize > 0n) {
    facts.push([
      t("metadataBrowser.indexSize"),
      formatBytes(props.table.indexSize),
    ]);
  }
  if (props.table.charset) {
    facts.push([t("metadataBrowser.characterSet"), props.table.charset]);
  }
  if (props.table.collation) {
    facts.push([t("metadataBrowser.collation"), props.table.collation]);
  }
  if (props.table.primaryKeyType) {
    facts.push([
      t("metadataBrowser.primaryKeyType"),
      props.table.primaryKeyType,
    ]);
  }
  if (props.table.createOptions) {
    facts.push([t("metadataBrowser.createOptions"), props.table.createOptions]);
  }
  if (props.table.shardingInfo) {
    facts.push([t("metadataBrowser.shardingInfo"), props.table.shardingInfo]);
  }

  return [...parts, ...facts.map(([label, value]) => `${label} ${value}`)].join(
    " · "
  );
});
</script>
