<template>
	<div class="space-y-3">
		<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<div class="text-sm font-medium">{{ titleText }}</div>
				<div class="text-sm text-muted-foreground">
					{{ t("metadataBrowser.historyDescription") }}
				</div>
			</div>
			<Button
				variant="outline"
				size="sm"
				:disabled="isLoadingList || isLoadingDetail"
				@click="refreshHistory"
			>
				{{ t("metadataBrowser.refreshHistory") }}
			</Button>
		</div>

		<div
			v-if="isLoadingList && entries.length === 0"
			class="rounded-lg border p-8"
		>
			<div class="flex justify-center">
				<AppLoading />
			</div>
		</div>

		<div
			v-else-if="listError"
			class="rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive"
		>
			{{ listError }}
		</div>

		<div
			v-else-if="entries.length === 0"
			class="rounded-lg border p-6 text-sm text-muted-foreground"
		>
			{{ t("metadataBrowser.noHistory") }}
		</div>

		<div
			v-else
			class="grid gap-4 xl:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]"
		>
			<div class="space-y-2">
				<button
					v-for="entry in entries"
					:key="entryKey(entry)"
					type="button"
					class="w-full rounded-lg border p-3 text-left transition-colors hover:border-ring hover:bg-accent/40"
					:class="selectedEntryKey === entryKey(entry) ? 'border-primary bg-accent/60' : 'border-border'"
					@click="selectEntry(entry)"
				>
					<div class="flex items-start justify-between gap-3">
						<div class="space-y-1">
							<div class="flex flex-wrap items-center gap-2">
								<Badge :variant="operationVariant(entry.operation)">
									{{ operationLabel(entry.operation) }}
								</Badge>
								<span class="text-xs text-muted-foreground">
									{{ formatTimestamp(entry.eventTime) }}
								</span>
							</div>
							<div class="font-medium leading-5">{{ entry.summary || fallbackEntrySummary(entry) }}</div>
						</div>
						<div
							v-if="isLoadingEntry(entry)"
							class="shrink-0 text-xs text-muted-foreground"
						>
							{{ t("metadataBrowser.loadingHistoryEvent") }}
						</div>
					</div>
					<div
						v-if="entry.sectionChanges.length > 0"
						class="mt-3 flex flex-wrap gap-1.5"
					>
						<Badge
							v-for="sectionChange in entry.sectionChanges"
							:key="sectionChange.section"
							variant="outline"
							class="text-[11px]"
						>
							{{ formatSectionChange(sectionChange) }}
						</Badge>
					</div>
				</button>

				<Button
					v-if="nextPageToken"
					variant="outline"
					size="sm"
					class="w-full"
					:disabled="isLoadingMore"
					@click="loadMoreHistory"
				>
					{{ isLoadingMore ? t("metadataBrowser.loadingMoreHistory") : t("metadataBrowser.loadMoreHistory") }}
				</Button>
			</div>

			<div class="rounded-lg border bg-card p-4">
				<div
					v-if="isLoadingDetail && !selectedEvent"
					class="py-8"
				>
					<div class="flex justify-center">
						<AppLoading />
					</div>
				</div>

				<div
					v-else-if="detailError"
					class="rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive"
				>
					{{ detailError }}
				</div>

				<div
					v-else-if="selectedEvent && selectedEntry"
					class="space-y-4"
				>
					<div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
						<div class="space-y-1">
							<div class="flex flex-wrap items-center gap-2">
								<Badge :variant="operationVariant(selectedEntry.operation)">
									{{ operationLabel(selectedEntry.operation) }}
								</Badge>
								<span class="text-sm text-muted-foreground">
									{{ formatTimestamp(selectedEntry.eventTime) }}
								</span>
							</div>
							<div class="text-base font-semibold">
								{{ selectedEntry.summary || fallbackEntrySummary(selectedEntry) }}
							</div>
						</div>
					</div>

					<div class="grid gap-2 sm:grid-cols-3">
						<div class="rounded-md border px-3 py-2">
							<div class="text-xs text-muted-foreground">{{ t("metadataBrowser.historyEventTime") }}</div>
							<div class="text-sm font-medium">{{ formatTimestamp(selectedEntry.eventTime) }}</div>
						</div>
						<div class="rounded-md border px-3 py-2">
							<div class="text-xs text-muted-foreground">{{ t("metadataBrowser.historyValidFrom") }}</div>
							<div class="text-sm font-medium">{{ formatTimestamp(selectedEntry.validFrom) }}</div>
						</div>
						<div class="rounded-md border px-3 py-2">
							<div class="text-xs text-muted-foreground">{{ t("metadataBrowser.historyValidTo") }}</div>
							<div class="text-sm font-medium">{{ formatTimestamp(selectedEntry.validTo) || t("metadataBrowser.historyCurrent") }}</div>
						</div>
					</div>

					<div
						v-if="selectedEvent.changeGroups.length > 0"
						class="space-y-3"
					>
						<Collapsible
							v-for="(group, groupIndex) in selectedEvent.changeGroups"
							:key="`${group.section}:${groupIndex}`"
							:default-open="groupIndex === 0"
						>
							<div class="rounded-lg border">
								<CollapsibleTrigger class="flex w-full items-center justify-between gap-3 px-4 py-3 text-left hover:bg-accent/40">
									<div>
										<div class="font-medium">{{ sectionLabel(group.section) }}</div>
										<div class="text-sm text-muted-foreground">
											{{ t("metadataBrowser.historyChangeCount", { count: group.changes.length }) }}
										</div>
									</div>
									<Badge variant="outline">{{ group.changes.length }}</Badge>
								</CollapsibleTrigger>
								<CollapsibleContent class="border-t px-4 py-3">
									<div class="space-y-3">
										<div
											v-for="item in group.changes"
											:key="`${item.section}:${item.operation}:${item.key}`"
											class="rounded-md border bg-background p-3"
										>
											<div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
												<div class="space-y-1">
													<div class="flex flex-wrap items-center gap-2">
														<Badge :variant="operationVariant(item.operation)">
															{{ operationLabel(item.operation) }}
														</Badge>
														<span class="font-medium">{{ item.displayName || item.key || sectionLabel(item.section) }}</span>
													</div>
													<div class="text-sm text-muted-foreground">
														{{ item.summary || fallbackItemSummary(item) }}
													</div>
												</div>
											</div>

											<div
												v-if="item.fieldChanges.length > 0"
												class="mt-3 space-y-2"
											>
												<div class="text-xs font-medium uppercase tracking-wide text-muted-foreground">
													{{ t("metadataBrowser.historyFieldChanges") }}
												</div>
												<Table>
													<TableHeader>
														<TableRow>
															<TableHead>{{ t("metadataBrowser.historyField") }}</TableHead>
															<TableHead>{{ t("metadataBrowser.historyBefore") }}</TableHead>
															<TableHead>{{ t("metadataBrowser.historyAfter") }}</TableHead>
														</TableRow>
													</TableHeader>
													<TableBody>
														<TableRow
															v-for="fieldChange in item.fieldChanges"
															:key="fieldChange.field"
														>
															<TableCell class="font-medium">{{ fieldChange.displayName || fieldChange.field }}</TableCell>
															<TableCell class="text-muted-foreground whitespace-pre-wrap break-all">{{ formatFieldValue(fieldChange.before) }}</TableCell>
															<TableCell class="text-muted-foreground whitespace-pre-wrap break-all">{{ formatFieldValue(fieldChange.after) }}</TableCell>
														</TableRow>
													</TableBody>
												</Table>
											</div>

											<div
												v-if="describeSnapshot(item.before) || describeSnapshot(item.after)"
												class="mt-3 grid gap-2 lg:grid-cols-2"
											>
												<div
													v-if="describeSnapshot(item.before)"
													class="rounded-md border bg-muted/20 p-3"
												>
													<div class="text-xs font-medium uppercase tracking-wide text-muted-foreground">{{ t("metadataBrowser.historyBefore") }}</div>
													<div class="mt-1 text-sm text-foreground whitespace-pre-wrap break-all">{{ describeSnapshot(item.before) }}</div>
												</div>
												<div
													v-if="describeSnapshot(item.after)"
													class="rounded-md border bg-muted/20 p-3"
												>
													<div class="text-xs font-medium uppercase tracking-wide text-muted-foreground">{{ t("metadataBrowser.historyAfter") }}</div>
													<div class="mt-1 text-sm text-foreground whitespace-pre-wrap break-all">{{ describeSnapshot(item.after) }}</div>
												</div>
											</div>
										</div>
									</div>
								</CollapsibleContent>
							</div>
						</Collapsible>
					</div>

					<div
						v-else
						class="rounded-md border p-4 text-sm text-muted-foreground"
					>
						{{ t("metadataBrowser.noHistoryDetails") }}
					</div>
				</div>

				<div
					v-else
					class="rounded-md border p-6 text-sm text-muted-foreground"
				>
					{{ t("metadataBrowser.selectHistoryEntry") }}
				</div>
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { getMetadataHistoryEvent, listMetadataHistory } from "@/api/database";
import AppLoading from "@/components/common/AppLoading.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  type MetadataHistoryChangeItem,
  type MetadataHistoryChildSnapshot,
  type MetadataHistoryEvent,
  MetadataHistoryOperation,
  MetadataHistorySection,
  type MetadataHistorySectionChangeCount,
  type MetadataHistoryTimelineEntry,
  type MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import { extractErrorMessage } from "@/utils/error";

const props = withDefaults(
  defineProps<{
    guid: string;
    metaType: MetaType;
    title?: string;
    pageSize?: number;
  }>(),
  {
    title: "",
    pageSize: 20,
  }
);

const { t, locale } = useI18n();

const entries = ref<MetadataHistoryTimelineEntry[]>([]);
const nextPageToken = ref("");
const selectedEntryKey = ref("");
const eventCache = reactive(new Map<string, MetadataHistoryEvent>());
const isLoadingList = ref(false);
const isLoadingMore = ref(false);
const isLoadingDetail = ref(false);
const loadingEventKey = ref("");
const listError = ref<string | null>(null);
const detailError = ref<string | null>(null);

const titleText = computed(
  () => props.title || t("metadataBrowser.historyTitle")
);

const selectedEntry = computed(() => {
  return (
    entries.value.find((entry) => entryKey(entry) === selectedEntryKey.value) ??
    null
  );
});

const selectedEvent = computed(() => {
  if (!selectedEntryKey.value) {
    return null;
  }
  return eventCache.get(selectedEntryKey.value) ?? null;
});

watch(
  () => [props.guid, props.metaType] as const,
  async () => {
    await loadHistory(true);
  },
  { immediate: true }
);

function entryKey(entry: MetadataHistoryTimelineEntry): string {
  const eventTime = entry.eventTime;
  const seconds = eventTime?.seconds ?? 0;
  const nanos = eventTime?.nanos ?? 0;
  return `${entry.guid}:${entry.operation}:${seconds}:${nanos}`;
}

function isLoadingEntry(entry: MetadataHistoryTimelineEntry): boolean {
  return loadingEventKey.value === entryKey(entry);
}

async function refreshHistory() {
  await loadHistory(true);
}

async function loadHistory(reset: boolean) {
  const loadingRef = reset ? isLoadingList : isLoadingMore;
  loadingRef.value = true;
  if (reset) {
    listError.value = null;
    detailError.value = null;
    entries.value = [];
    nextPageToken.value = "";
    selectedEntryKey.value = "";
    eventCache.clear();
  }

  try {
    const response = await listMetadataHistory({
      guid: props.guid,
      metaType: props.metaType,
      pageSize: props.pageSize,
      pageToken: reset ? "" : nextPageToken.value,
    });
    entries.value = reset
      ? response.entries
      : [...entries.value, ...response.entries];
    nextPageToken.value = response.nextPageToken;
    if (reset && response.entries.length > 0) {
      await selectEntry(response.entries[0]);
    }
  } catch (error) {
    listError.value =
      extractErrorMessage(error) || t("metadataBrowser.historyFetchError");
  } finally {
    loadingRef.value = false;
  }
}

async function loadMoreHistory() {
  if (!nextPageToken.value || isLoadingMore.value) {
    return;
  }
  await loadHistory(false);
}

async function selectEntry(entry: MetadataHistoryTimelineEntry) {
  const key = entryKey(entry);
  selectedEntryKey.value = key;
  if (eventCache.has(key)) {
    detailError.value = null;
    return;
  }

  isLoadingDetail.value = true;
  loadingEventKey.value = key;
  detailError.value = null;
  try {
    const event = await getMetadataHistoryEvent({
      guid: props.guid,
      metaType: props.metaType,
      eventTime: entry.eventTime,
      operation: entry.operation,
    });
    eventCache.set(key, event);
  } catch (error) {
    detailError.value =
      extractErrorMessage(error) || t("metadataBrowser.historyEventFetchError");
  } finally {
    isLoadingDetail.value = false;
    loadingEventKey.value = "";
  }
}

function fallbackEntrySummary(entry: MetadataHistoryTimelineEntry): string {
  return operationLabel(entry.operation);
}

function fallbackItemSummary(item: MetadataHistoryChangeItem): string {
  return operationLabel(item.operation);
}

function operationLabel(operation: MetadataHistoryOperation): string {
  switch (operation) {
    case MetadataHistoryOperation.CREATED:
      return t("metadataBrowser.historyOperationCreated");
    case MetadataHistoryOperation.UPDATED:
      return t("metadataBrowser.historyOperationUpdated");
    case MetadataHistoryOperation.DELETED:
      return t("metadataBrowser.historyOperationDeleted");
    default:
      return t("metadataBrowser.unknown");
  }
}

function operationVariant(
  operation: MetadataHistoryOperation
): "success" | "secondary" | "destructive" | "outline" {
  if (operation === MetadataHistoryOperation.CREATED) {
    return "success";
  }
  if (operation === MetadataHistoryOperation.DELETED) {
    return "destructive";
  }
  if (operation === MetadataHistoryOperation.UPDATED) {
    return "secondary";
  }
  return "outline";
}

function sectionLabel(section: MetadataHistorySection): string {
  switch (section) {
    case MetadataHistorySection.SELF:
      return t("metadataBrowser.historySectionSelf");
    case MetadataHistorySection.COLUMN:
      return t("metadataBrowser.historySectionColumn");
    case MetadataHistorySection.INDEX:
      return t("metadataBrowser.historySectionIndex");
    case MetadataHistorySection.FOREIGN_KEY:
      return t("metadataBrowser.historySectionForeignKey");
    case MetadataHistorySection.CHECK_CONSTRAINT:
      return t("metadataBrowser.historySectionCheckConstraint");
    case MetadataHistorySection.PARTITION:
      return t("metadataBrowser.historySectionPartition");
    case MetadataHistorySection.TRIGGER:
      return t("metadataBrowser.historySectionTrigger");
    case MetadataHistorySection.RULE:
      return t("metadataBrowser.historySectionRule");
    case MetadataHistorySection.TAG:
      return t("metadataBrowser.historySectionTag");
    case MetadataHistorySection.ATTRIBUTE:
      return t("metadataBrowser.historySectionAttribute");
    default:
      return t("metadataBrowser.historySectionChange");
  }
}

function pluralizeSection(label: string, count: number): string {
  if (locale.value.startsWith("zh")) {
    return label;
  }
  if (count === 1) {
    return label;
  }
  if (label.endsWith("y")) {
    return `${label.slice(0, -1)}ies`;
  }
  return `${label}s`;
}

function formatSectionChange(
  change: MetadataHistorySectionChangeCount
): string {
  const label = sectionLabel(change.section);
  const parts: string[] = [];
  if (change.added > 0) {
    parts.push(`+${change.added} ${pluralizeSection(label, change.added)}`);
  }
  if (change.updated > 0) {
    parts.push(`~${change.updated} ${pluralizeSection(label, change.updated)}`);
  }
  if (change.removed > 0) {
    parts.push(`-${change.removed} ${pluralizeSection(label, change.removed)}`);
  }
  return parts.join(" ");
}

function formatTimestamp(timestamp?: Timestamp): string {
  if (!timestamp) {
    return "";
  }
  const milliseconds =
    Number(timestamp.seconds ?? 0) * 1000 +
    Math.floor((timestamp.nanos ?? 0) / 1_000_000);
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) {
    return "";
  }
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(milliseconds));
}

function formatFieldValue(value: string): string {
  return value === "" ? "-" : value;
}

function describeSnapshot(snapshot?: MetadataHistoryChildSnapshot): string {
  if (!snapshot || !snapshot.metadata.case) {
    return "";
  }

  switch (snapshot.metadata.case) {
    case "columnMetadata": {
      const column = snapshot.metadata.value;
      return [
        column.name,
        column.type,
        column.nullable
          ? t("metadataBrowser.historyNullable")
          : t("metadataBrowser.historyNotNull"),
        column.default
          ? `${t("metadataBrowser.defaultValue")}: ${column.default}`
          : "",
      ]
        .filter(Boolean)
        .join(" · ");
    }
    case "indexMetadata": {
      const index = snapshot.metadata.value;
      return [
        index.name,
        index.type,
        index.expressions.join(", "),
        index.unique ? t("metadataBrowser.unique") : "",
        index.primary ? t("metadataBrowser.primary") : "",
      ]
        .filter(Boolean)
        .join(" · ");
    }
    case "foreignKeyMetadata": {
      const foreignKey = snapshot.metadata.value;
      const reference = `${foreignKey.referencedSchema ? `${foreignKey.referencedSchema}.` : ""}${foreignKey.referencedTable} (${foreignKey.referencedColumns.join(", ")})`;
      return [foreignKey.name, foreignKey.columns.join(", "), reference]
        .filter(Boolean)
        .join(" → ");
    }
    case "checkConstraintMetadata": {
      const constraint = snapshot.metadata.value;
      return [constraint.name, constraint.expression]
        .filter(Boolean)
        .join(" · ");
    }
    case "partitionMetadata": {
      const partition = snapshot.metadata.value;
      return [
        partition.name,
        String(partition.type),
        partition.expression,
        partition.value,
      ]
        .filter(Boolean)
        .join(" · ");
    }
    case "triggerMetadata": {
      const trigger = snapshot.metadata.value;
      return [trigger.name, trigger.event, trigger.timing, trigger.comment]
        .filter(Boolean)
        .join(" · ");
    }
    case "ruleMetadata": {
      const rule = snapshot.metadata.value;
      return [rule.name, rule.event, rule.condition]
        .filter(Boolean)
        .join(" · ");
    }
    default:
      return "";
  }
}
</script>