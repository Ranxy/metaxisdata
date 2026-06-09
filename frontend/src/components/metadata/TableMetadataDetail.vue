<template>
  <div class="p-4 space-y-6">
    <div class="space-y-1">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <div class="text-lg font-semibold wrap-break-word">
          {{ table.name }}
        </div>
        <div
          v-if="table.userComment || table.comment"
          class="text-sm text-muted-foreground wrap-break-word max-w-xl"
        >
          <ExpandableText
            :text="table.userComment || table.comment"
            :item-name="table.name"
            :dialog-title="t('metadataBrowser.comment')"
          />
        </div>
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
          @click="$router.push(`/explain-sql/${guid}`)"
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
      v-if="guid"
      class="inline-flex rounded-lg border bg-muted/30 p-1"
    >
      <button
        type="button"
        class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
        :class="activeTab === 'details' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
        @click="activeTab = 'details'"
      >
        {{ t("metadataBrowser.tableDetail") }}
      </button>
      <button
        type="button"
        class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
        :class="activeTab === 'history' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
        @click="activeTab = 'history'"
      >
        {{ t("metadataBrowser.historyTitle") }}
      </button>
    </div>

    <template v-if="!guid || activeTab === 'details'">
    <div class="space-y-2">
      <div class="text-sm font-medium">{{ t("metadataBrowser.tableInfo") }}</div>
      <div class="flex flex-wrap gap-2">
        <div
          v-if="isMySQLFamily"
          class="rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.engine") }}</div>
          <div class="text-sm font-medium">{{ table.engine || "-" }}</div>
        </div>

        <div
          v-if="isPostgres"
          class="rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.owner") }}</div>
          <div class="text-sm font-medium">{{ table.owner || "-" }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.rowCount") }}</div>
          <div class="text-sm font-medium">{{ formatNumber(table.rowCount) }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.dataSize") }}</div>
          <div class="text-sm font-medium">{{ formatBytes(table.dataSize) }}</div>
        </div>

        <div class="rounded-md border px-3 py-2">
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.indexSize") }}</div>
          <div class="text-sm font-medium">{{ formatBytes(table.indexSize) }}</div>
        </div>

        <div
          v-if="table.charset"
          class="rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.characterSet") }}</div>
          <div class="text-sm font-medium">{{ table.charset }}</div>
        </div>

        <div
          v-if="table.collation"
          class="rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.collation") }}</div>
          <div class="text-sm font-medium">{{ table.collation }}</div>
        </div>

        <div
          v-if="table.createOptions"
          class="rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.createOptions") }}</div>
          <div class="text-sm font-medium">{{ table.createOptions }}</div>
        </div>

        <div
          v-if="table.primaryKeyType"
          class="rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.primaryKeyType") }}</div>
          <div class="text-sm font-medium">{{ table.primaryKeyType }}</div>
        </div>

        <div
          v-if="table.shardingInfo"
          class="rounded-md border px-3 py-2"
        >
          <div class="text-xs text-muted-foreground">{{ t("metadataBrowser.shardingInfo") }}</div>
          <div class="text-sm font-medium">{{ table.shardingInfo }}</div>
        </div>
      </div>
    </div>

    <TableLineageSection
      v-if="guid"
      :guid="guid"
      :meta-type="MetaType.TABLE"
      :title="t('metadataBrowser.relatedLineageAnalysis')"
    />

    <div class="space-y-2">
      <div class="flex items-center justify-between">
        <div class="text-sm font-medium">{{ t("metadataBrowser.columns") }}</div>
        <div class="flex items-center gap-2">
          <Input
            v-model="columnSearch"
            class="h-9 w-64"
            :placeholder="t('metadataBrowser.searchColumnsPlaceholder')"
          />
          <Badge variant="outline">
            {{ filteredColumns.length }} / {{ table.columns.length }}
            {{ t("metadataBrowser.columnsCount") }}
          </Badge>
        </div>
      </div>

      <Table v-if="filteredColumns.length > 0">
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.columnName") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.columnType") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.nullable") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.defaultValue") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.comment") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="col in filteredColumns"
            :key="`${col.position}:${col.name}`"
            :ref="(el) => setColumnRowRef(col.name, el)"
            :class="{ 'bg-accent/50': selectedColumnName === col.name }"
          >
            <TableCell class="font-medium">{{ col.name }}</TableCell>
            <TableCell class="text-muted-foreground">{{ col.type || "-" }}</TableCell>
            <TableCell>
              <Badge
                :variant="col.nullable ? 'secondary' : 'success'"
                class="whitespace-nowrap"
              >
                {{ col.nullable ? t("metadataBrowser.yes") : t("metadataBrowser.no") }}
              </Badge>
            </TableCell>
            <TableCell class="text-muted-foreground">{{ col.default || "-" }}</TableCell>
            <TableCell class="text-muted-foreground max-w-md">
              <ExpandableText
                :text="col.userComment || col.comment"
                :item-name="col.name"
                :dialog-title="t('metadataBrowser.comment')"
              />
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>

      <div
        v-else
        class="text-sm text-muted-foreground"
      >
        {{ columnSearch ? t("metadataBrowser.noMatchedColumns") : t("metadataBrowser.noColumns") }}
      </div>
    </div>

    <div class="space-y-2">
      <div class="flex items-center justify-between">
        <div class="text-sm font-medium">{{ t("metadataBrowser.indexes") }}</div>
        <Badge variant="outline">
          {{ table.indexes.length }} {{ t("metadataBrowser.indexesCount") }}
        </Badge>
      </div>

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
            <TableCell class="text-muted-foreground max-w-md truncate">
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
    </div>

    <div
      v-if="table.foreignKeys.length > 0"
      class="space-y-2"
    >
      <div class="text-sm font-medium">{{ t("metadataBrowser.foreignKeys") }}</div>
      <Table>
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
    </div>

    <div
      v-if="table.checkConstraints.length > 0"
      class="space-y-2"
    >
      <div class="text-sm font-medium">{{ t("metadataBrowser.checkConstraints") }}</div>
      <Table>
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
            <TableCell class="text-muted-foreground max-w-xl truncate">{{ cc.expression }}</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>

    <div
      v-if="table.partitions.length > 0"
      class="space-y-2"
    >
      <div class="text-sm font-medium">{{ t("metadataBrowser.partitions") }}</div>
      <Table>
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
            <TableCell class="text-muted-foreground max-w-xl truncate">{{ p.expression || "-" }}</TableCell>
            <TableCell class="text-muted-foreground">{{ p.value || "-" }}</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    </template>

    <MetadataHistorySection
      v-else
      :guid="guid"
      :meta-type="MetaType.TABLE"
    />
  </div>
</template>

<script setup lang="ts">
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
import { Input } from "@/components/ui/input";
import { Sparkles } from "lucide-vue-next";
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
  type ColumnMetadata,
  MetaType,
  type TableMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import ExpandableText from "./ExpandableText.vue";
import SchemaDefinitionDialog from "./SchemaDefinitionDialog.vue";

const props = defineProps<{
  table: TableMetadata;
  instanceEngine?: Engine | null;
  guid?: string;
  selectedColumnName?: string;
}>();

const { t } = useI18n();

const activeTab = ref<"details" | "history">("details");
const columnSearch = ref("");
const columnRowRefs = new Map<string, Element>();

const selectedColumnName = computed(
  () => props.selectedColumnName?.trim() || ""
);

const filteredColumns = computed((): ColumnMetadata[] => {
  const q = columnSearch.value.trim().toLowerCase();
  if (!q) return props.table.columns;
  return props.table.columns.filter((c) => {
    const comment = (c.userComment || c.comment || "").toLowerCase();
    return c.name.toLowerCase().includes(q) || comment.includes(q);
  });
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
    await focusSelectedColumn();
  },
  { immediate: true }
);

const isMySQLFamily = computed(() => {
  if (!props.instanceEngine) return false;
  return (
    props.instanceEngine === Engine.MYSQL ||
    props.instanceEngine === Engine.MARIADB ||
    props.instanceEngine === Engine.TIDB
  );
});

const isPostgres = computed(() => props.instanceEngine === Engine.POSTGRES);

const summaryLine = computed(() => {
  const parts: string[] = [];
  if (props.table.columns.length > 0) {
    parts.push(`${props.table.columns.length} ${t("metadataBrowser.columns")}`);
  }
  if (props.table.indexes.length > 0) {
    parts.push(`${props.table.indexes.length} ${t("metadataBrowser.indexes")}`);
  }
  if (props.table.foreignKeys.length > 0) {
    parts.push(
      `${props.table.foreignKeys.length} ${t("metadataBrowser.foreignKeys")}`
    );
  }
  if (props.table.partitions.length > 0) {
    parts.push(
      `${props.table.partitions.length} ${t("metadataBrowser.partitions")}`
    );
  }
  return parts.join(" · ");
});

function formatNumber(value: bigint): string {
  return new Intl.NumberFormat().format(Number(value));
}

function formatBytes(bytes: bigint): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = Number(bytes);
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }
  return `${size.toFixed(1)} ${units[unitIndex]}`;
}
</script>
