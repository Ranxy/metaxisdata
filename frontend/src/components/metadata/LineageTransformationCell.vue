<template>
  <div
    v-if="summaryText"
    class="flex min-w-0 items-center gap-2"
  >
    <span
      class="min-w-0 flex-1 truncate"
      :title="summaryText"
    >
      {{ summaryText }}
    </span>
    <Dialog
      v-if="hasDetail"
      v-model:open="detailOpen"
    >
      <DialogTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          class="h-7 px-2"
        >
          {{ t("metadataBrowser.viewDetails") }}
        </Button>
      </DialogTrigger>
      <DialogContent class="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{{ dialogTitle }}</DialogTitle>
          <DialogDescription v-if="itemName">
            {{ itemName }}
          </DialogDescription>
        </DialogHeader>

        <div class="max-h-[70vh] space-y-4 overflow-y-auto pr-1">
          <template v-if="parsedItems.length > 0">
            <div
              v-for="(item, index) in parsedItems"
              :key="buildTransformationKey(item, index)"
              class="space-y-3 rounded-md border p-4"
            >
              <div class="flex items-center justify-between gap-3">
                <Badge variant="secondary">
                  {{ item.operation || t("metadataBrowser.unknown") }}
                </Badge>
                <span class="text-xs text-muted-foreground">
                  {{ t("metadataBrowser.transformationStep", { index: index + 1 }) }}
                </span>
              </div>

              <div class="grid gap-3 sm:grid-cols-[140px_minmax(0,1fr)]">
                <template
                  v-for="detail in buildTransformationDetails(item)"
                  :key="`${buildTransformationKey(item, index)}:${detail.label}`"
                >
                  <div class="text-sm font-medium text-muted-foreground">
                    {{ detail.label }}
                  </div>
                  <div class="min-w-0 text-sm break-words whitespace-pre-wrap">
                    {{ detail.value }}
                  </div>
                </template>
              </div>
            </div>
          </template>

          <div
            v-else
            class="rounded-md border p-4"
          >
            <div class="grid gap-3 sm:grid-cols-[140px_minmax(0,1fr)]">
              <div class="text-sm font-medium text-muted-foreground">
                {{ t("metadataBrowser.rawTransformation") }}
              </div>
              <div class="min-w-0 text-sm break-words whitespace-pre-wrap">
                {{ rawText }}
              </div>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  </div>
  <span v-else>-</span>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";

type LineageTransformation = {
  arguments?: string[];
  condition?: string;
  expression?: string;
  function_name?: string;
  group_keys?: string[];
  op_type?: string;
  operation?: string;
  order_by?: string[];
  partition_by?: string[];
};

type TransformationDetail = {
  label: string;
  value: string;
};

const props = defineProps<{
  text?: string;
  itemName?: string;
  dialogTitle?: string;
}>();

const { t } = useI18n();

const detailOpen = ref(false);

const rawText = computed(() => normalizeRawText(props.text));

const parsedItems = computed(() => parseTransformations(rawText.value));

const summaryText = computed(() => {
  if (parsedItems.value.length > 0) {
    return parsedItems.value.map((item) => buildSummary(item)).join(" | ");
  }

  return rawText.value;
});

const hasDetail = computed(() => Boolean(rawText.value));

const dialogTitle = computed(
  () => props.dialogTitle || t("metadataBrowser.transformationDetails")
);

function normalizeRawText(text: string | undefined): string {
  const trimmed = text?.trim() ?? "";
  if (!trimmed || trimmed === "[]") {
    return "";
  }
  return trimmed;
}

function parseTransformations(text: string): LineageTransformation[] {
  if (!text) {
    return [];
  }

  try {
    const value: unknown = JSON.parse(text);
    if (!Array.isArray(value)) {
      return [];
    }

    return value.filter(isLineageTransformation);
  } catch {
    return [];
  }
}

function isLineageTransformation(
  value: unknown
): value is LineageTransformation {
  return typeof value === "object" && value !== null;
}

function buildSummary(item: LineageTransformation): string {
  const operation = item.operation || t("metadataBrowser.unknown");

  switch (item.operation) {
    case "FUNCTION":
      return item.function_name
        ? `${operation}: ${item.function_name}`
        : `${operation}: ${shorten(item.expression)}`;
    case "AGGREGATE":
    case "WINDOW":
      return item.function_name
        ? `${operation}: ${item.function_name}`
        : operation;
    case "OPERATOR":
      return item.op_type ? `${operation}: ${item.op_type}` : operation;
    case "DELETE":
      return item.condition
        ? `${operation}: ${shorten(item.condition)}`
        : operation;
    case "PROJECT":
    case "CASE":
      return item.expression
        ? `${operation}: ${shorten(item.expression)}`
        : operation;
    default:
      return item.expression
        ? `${operation}: ${shorten(item.expression)}`
        : operation;
  }
}

function shorten(value: string | undefined, maxLength = 72): string {
  const text = value?.trim() ?? "";
  if (!text) {
    return "";
  }

  if (text.length <= maxLength) {
    return text;
  }

  return `${text.slice(0, maxLength - 1)}…`;
}

function buildTransformationDetails(
  item: LineageTransformation
): TransformationDetail[] {
  const details: TransformationDetail[] = [];

  appendDetail(details, t("metadataBrowser.operation"), item.operation);
  appendDetail(details, t("metadataBrowser.expression"), item.expression);
  appendDetail(details, t("metadataBrowser.functionName"), item.function_name);
  appendListDetail(details, t("metadataBrowser.arguments"), item.arguments);
  appendListDetail(details, t("metadataBrowser.groupKeys"), item.group_keys);
  appendListDetail(
    details,
    t("metadataBrowser.partitionBy"),
    item.partition_by
  );
  appendListDetail(details, t("metadataBrowser.orderBy"), item.order_by);
  appendDetail(details, t("metadataBrowser.operatorType"), item.op_type);
  appendDetail(details, t("metadataBrowser.condition"), item.condition);

  if (details.length === 0) {
    details.push({
      label: t("metadataBrowser.rawTransformation"),
      value: JSON.stringify(item, null, 2),
    });
  }

  return details;
}

function appendDetail(
  details: TransformationDetail[],
  label: string,
  value: string | undefined
) {
  const normalized = value?.trim();
  if (!normalized) {
    return;
  }

  details.push({ label, value: normalized });
}

function appendListDetail(
  details: TransformationDetail[],
  label: string,
  value: string[] | undefined
) {
  if (!value || value.length === 0) {
    return;
  }

  details.push({ label, value: value.join("\n") });
}

function buildTransformationKey(
  item: LineageTransformation,
  index: number
): string {
  return [
    item.operation,
    item.function_name,
    item.op_type,
    item.expression,
    item.condition,
    String(index),
  ].join(":");
}
</script>