<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <div class="text-sm font-medium">{{ t("metadataBrowser.versionDiffTitle") }}</div>
        <div class="text-sm text-muted-foreground">
          {{ t("metadataBrowser.versionDiffDescription") }}
        </div>
      </div>
    </div>

    <div class="grid gap-4 sm:grid-cols-2">
      <div class="space-y-2">
        <label class="text-xs font-medium">{{ t("metadataBrowser.sourceVersion") }}</label>
        <Select v-model="sourceEntryKey">
          <SelectTrigger class="w-full">
            <SelectValue :placeholder="t('metadataBrowser.selectSourceVersion')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="entry in entries"
              :key="entryKey(entry)"
              :value="entryKey(entry)"
            >
              {{ formatTimestamp(entry.eventTime) }} · {{ entry.summary || fallbackEntrySummary(entry) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div class="space-y-2">
        <label class="text-xs font-medium">{{ t("metadataBrowser.targetVersion") }}</label>
        <Select v-model="targetEntryKey">
          <SelectTrigger class="w-full">
            <SelectValue :placeholder="t('metadataBrowser.selectTargetVersion')" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="entry in entries"
              :key="entryKey(entry)"
              :value="entryKey(entry)"
            >
              {{ formatTimestamp(entry.eventTime) }} · {{ entry.summary || fallbackEntrySummary(entry) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
    </div>

    <div class="flex gap-2">
      <Button
        :disabled="!sourceEntryKey || !targetEntryKey || sourceEntryKey === targetEntryKey || isLoading"
        @click="runDiff"
      >
        {{ isLoading ? t("metadataBrowser.comparing") : t("metadataBrowser.compareVersions") }}
      </Button>
    </div>

    <div v-if="error" class="rounded-lg border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
      {{ error }}
    </div>

    <div v-if="result" class="space-y-4">
      <div class="rounded-lg border bg-card p-4">
        <div class="text-sm font-medium mb-2">{{ t("metadataBrowser.diffSummary") }}</div>
        <pre class="text-sm whitespace-pre-wrap text-muted-foreground">{{ result.diffSummary }}</pre>
      </div>

      <div v-if="result.ddl" class="rounded-lg border bg-card p-4">
        <div class="flex items-center justify-between mb-2">
          <div class="text-sm font-medium">{{ t("metadataBrowser.migrationDdl") }}</div>
          <Button variant="outline" size="sm" @click="copyDdl">
            {{ copied ? t("metadataBrowser.copied") : t("metadataBrowser.copyDdl") }}
          </Button>
        </div>
        <pre class="text-sm whitespace-pre-wrap font-mono bg-muted/30 rounded p-3 overflow-auto max-h-96">{{ result.ddl }}</pre>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import { diffMetadata } from "@/api/database";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { MetadataHistoryTimelineEntry } from "@/types/proto-es/v1/database_service_pb";
import { extractErrorMessage } from "@/utils/error";

const props = defineProps<{
  guid: string;
  entries: MetadataHistoryTimelineEntry[];
}>();

const { t, locale } = useI18n();

const sourceEntryKey = ref("");
const targetEntryKey = ref("");
const isLoading = ref(false);
const error = ref<string | null>(null);
const result = ref<{ diffSummary: string; ddl: string } | null>(null);
const copied = ref(false);

function entryKey(entry: MetadataHistoryTimelineEntry): string {
  const eventTime = entry.eventTime;
  const seconds = eventTime?.seconds ?? 0;
  const nanos = eventTime?.nanos ?? 0;
  return `${entry.operation}:${seconds}:${nanos}`;
}

function getEntryByKey(key: string): MetadataHistoryTimelineEntry | undefined {
  return props.entries.find((e) => entryKey(e) === key);
}

function fallbackEntrySummary(entry: MetadataHistoryTimelineEntry): string {
  return entry.operation === 1
    ? t("metadataBrowser.created")
    : entry.operation === 3
      ? t("metadataBrowser.deleted")
      : t("metadataBrowser.updated");
}

function formatTimestamp(timestamp?: Timestamp): string {
  if (!timestamp) return "";
  const ms =
    Number(timestamp.seconds ?? 0) * 1000 +
    Math.floor((timestamp.nanos ?? 0) / 1_000_000);
  if (!Number.isFinite(ms) || ms <= 0) return "";
  return new Intl.DateTimeFormat(locale.value, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(ms));
}

async function runDiff() {
  const sourceEntry = getEntryByKey(sourceEntryKey.value);
  const targetEntry = getEntryByKey(targetEntryKey.value);
  if (!sourceEntry || !targetEntry) return;

  isLoading.value = true;
  error.value = null;
  result.value = null;

  try {
    const sourceTime = sourceEntry.validFrom
      ? new Date(Number(sourceEntry.validFrom.seconds) * 1000)
      : undefined;
    const targetTime = targetEntry.validFrom
      ? new Date(Number(targetEntry.validFrom.seconds) * 1000)
      : undefined;

    const response = await diffMetadata({
      guid: props.guid,
      sourceTime,
      targetTime,
    });

    result.value = {
      diffSummary: response.diffSummary,
      ddl: response.ddl,
    };
  } catch (e) {
    error.value = extractErrorMessage(e) || t("metadataBrowser.diffError");
  } finally {
    isLoading.value = false;
  }
}

async function copyDdl() {
  if (!result.value?.ddl) return;
  try {
    await navigator.clipboard.writeText(result.value.ddl);
    copied.value = true;
    setTimeout(() => {
      copied.value = false;
    }, 2000);
  } catch {
    // Clipboard not available
  }
}
</script>
