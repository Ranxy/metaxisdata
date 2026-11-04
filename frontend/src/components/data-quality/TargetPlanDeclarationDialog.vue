<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-2xl">
      <div class="flex max-h-[90vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>
            {{ t("dataQuality.planDeclaration.title") }}
          </DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.planDeclaration.description") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-4 overflow-auto px-6 py-5">
          <p
            v-if="warning"
            class="text-sm text-destructive"
          >
            {{ warning }}
          </p>

          <Alert
            v-if="error"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>
              {{ t("dataQuality.planDeclaration.saveFailed") }}
            </AlertTitle>
            <AlertDescription class="break-words">
              {{ error }}
            </AlertDescription>
          </Alert>

          <p class="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground">
            {{
              t("dataQuality.planDeclaration.baseHint", {
                version: contract?.publishedVersion ?? 0,
              })
            }}
          </p>

          <div
            v-for="(row, index) in rows"
            :key="row.key"
            class="flex flex-wrap items-end gap-2 rounded-md border p-2"
          >
            <div class="min-w-40 flex-1 space-y-1">
              <Label :for="`dq-plan-property-${row.key}`">
                {{ t("dataQuality.planDeclaration.property") }}
              </Label>
              <Select
                :model-value="String(row.property)"
                @update:model-value="(value) => setProperty(index, value)"
              >
                <SelectTrigger :id="`dq-plan-property-${row.key}`">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="option in propertyOptions"
                    :key="option.value"
                    :value="String(option.value)"
                  >
                    {{ t(option.key) }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="min-w-32 flex-1 space-y-1">
              <Label :for="`dq-plan-column-${row.key}`">
                {{ t("dataQuality.planDeclaration.column") }}
              </Label>
              <Input
                :id="`dq-plan-column-${row.key}`"
                v-model="row.column"
                list="dq-plan-declaration-columns"
                :placeholder="t('dataQuality.planDeclaration.columnPlaceholder')"
              />
            </div>
            <div class="w-24 space-y-1">
              <Label :for="`dq-plan-min-checks-${row.key}`">
                {{ t("dataQuality.planDeclaration.minChecks") }}
              </Label>
              <Input
                :id="`dq-plan-min-checks-${row.key}`"
                v-model.number="row.minChecks"
                type="number"
                min="1"
                max="10"
                @blur="clampMinChecks(index)"
              />
            </div>
            <Button
              variant="ghost"
              size="icon"
              :title="t('dataQuality.planDeclaration.remove')"
              :aria-label="t('dataQuality.planDeclaration.remove')"
              @click="removeRow(index)"
            >
              <Trash2 class="h-4 w-4" />
            </Button>
          </div>
          <datalist id="dq-plan-declaration-columns">
            <option
              v-for="column in columns"
              :key="column.name"
              :value="column.name"
            />
          </datalist>

          <div class="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              :disabled="saving"
              @click="addRow"
            >
              <Plus class="mr-1 h-4 w-4" />
              {{ t("dataQuality.planDeclaration.add") }}
            </Button>
            <Button
              v-if="hasDeclaration"
              variant="ghost"
              size="sm"
              :disabled="saving"
              @click="clearRows"
            >
              {{ t("dataQuality.planDeclaration.clear") }}
            </Button>
          </div>
          <p
            v-if="rows.length === 0"
            class="text-xs text-muted-foreground"
          >
            {{ t("dataQuality.planDeclaration.noDeclarations") }}
          </p>
        </div>

        <DialogFooter class="shrink-0 border-t px-6 py-4">
          <Button
            variant="outline"
            :disabled="saving"
            @click="close"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="saving || !contract"
            @click="save"
          >
            {{ t("dataQuality.planDeclaration.save") }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { AlertTriangle, Plus, Trash2 } from "lucide-vue-next";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { updateQualityContract } from "@/api/data-quality";
import { listAllMetadata } from "@/api/database";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import {
  type QualityContract,
  QualityContractSettingsSchema,
  QualityCoverageProperty,
  type QualityPlan,
  type QualityPlanCoverage,
  QualityPlanCoverageSchema,
  QualityPlanSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { qualityCoveragePropertyKey } from "@/utils/dataQuality";

/**
 * Edit the target's plan declaration: the properties it promises to watch.
 *
 * The declaration is the target's current plan, not a throwaway draft: saving it
 * records an unpublished difference, and publishing it records the declarations in
 * the next contract version. Saving an empty declaration returns the target to the
 * plan derived from the intent and the live checks.
 *
 * Column names are suggestions from the target's metadata; the target is the
 * authority and rejects a column it does not have.
 */

const props = defineProps<{
  open: boolean;
  targetGuid: string;
  metaType: MetaType;
  contract?: QualityContract;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  updated: [contract: QualityContract];
  /** The declaration changed underneath the editor; the page re-reads the contract. */
  refresh: [];
}>();

interface DeclarationRow {
  key: number;
  property: QualityCoverageProperty;
  column: string;
  minChecks: number;
}

const { t } = useI18n();
const { formatError } = useErrorMessage();

const rows = ref<DeclarationRow[]>([]);
const columns = ref<{ name: string }[]>([]);
const error = ref("");
const warning = ref("");
const saving = ref(false);
let nextKey = 1;

const propertyOptions = [
  QualityCoverageProperty.VOLUME,
  QualityCoverageProperty.FRESHNESS,
  QualityCoverageProperty.NULL_RATE,
  QualityCoverageProperty.UNIQUENESS,
  QualityCoverageProperty.ACCEPTED_VALUES,
  QualityCoverageProperty.FORMAT,
  QualityCoverageProperty.RANGE,
  QualityCoverageProperty.SCHEMA_STABILITY,
].map((value) => ({ value, key: qualityCoveragePropertyKey(value) }));

const hasDeclaration = computed(
  () => (props.contract?.stagedPlan?.coverage.length ?? 0) > 0
);

/** The declaration to edit: the current one, else the derived coverage. */
function declarationsFromContract(
  contract?: QualityContract
): DeclarationRow[] {
  const coverage: QualityPlanCoverage[] =
    (contract?.stagedPlan?.coverage.length ?? 0) > 0
      ? (contract?.stagedPlan?.coverage ?? [])
      : (contract?.plan?.coverage ?? []);
  return coverage.map((declaration) => ({
    key: nextKey++,
    property: declaration.property || QualityCoverageProperty.VOLUME,
    column: declaration.column,
    minChecks: declaration.minChecks || 1,
  }));
}

function reset() {
  rows.value = declarationsFromContract(props.contract);
  columns.value = [];
  error.value = "";
  warning.value = "";
  saving.value = false;
}

function addRow() {
  warning.value = "";
  rows.value.push({
    key: nextKey++,
    property: QualityCoverageProperty.VOLUME,
    column: "",
    minChecks: 1,
  });
}

function removeRow(index: number) {
  warning.value = "";
  rows.value.splice(index, 1);
}

function clearRows() {
  warning.value = "";
  rows.value = [];
}

/** A minimum outside 1–10 is clamped where it is typed, not only on save. */
function clampMinChecks(index: number) {
  const row = rows.value[index];
  if (!row) {
    return;
  }
  row.minChecks = Math.min(
    10,
    Math.max(1, Math.round(Number(row.minChecks) || 1))
  );
}

function setProperty(index: number, value: unknown) {
  warning.value = "";
  const row = rows.value[index];
  if (row) {
    row.property = Number(value) as QualityCoverageProperty;
  }
}

/** Column suggestions; a failure is not fatal because the field is free text. */
async function loadColumns() {
  const guid = props.targetGuid;
  try {
    const items = await listAllMetadata({
      parentGuid: guid,
      metaType: MetaType.COLUMN,
    });
    if (!props.open || guid !== props.targetGuid) {
      return;
    }
    columns.value = items.map((item) => ({
      name:
        (item.type.value as { name?: string } | undefined)?.name ?? item.guid,
    }));
  } catch {
    if (props.open && guid === props.targetGuid) {
      columns.value = [];
    }
  }
}

watch(
  () => props.open,
  (isOpen) => {
    reset();
    if (isOpen) {
      void loadColumns();
    }
  },
  { immediate: true }
);

function close() {
  emit("update:open", false);
}

function onOpenChange(value: boolean) {
  if (value) {
    return;
  }
  close();
}

function buildDeclaration(): QualityPlan {
  return create(QualityPlanSchema, {
    coverage: rows.value.map((row) =>
      create(QualityPlanCoverageSchema, {
        property: row.property,
        column: row.column.trim(),
        minChecks: Math.min(10, Math.max(1, Number(row.minChecks) || 1)),
      })
    ),
  });
}

function duplicateDeclaration(): boolean {
  const seen = new Set<string>();
  for (const row of rows.value) {
    const key = `${row.property}|${row.column.trim()}`;
    if (seen.has(key)) {
      return true;
    }
    seen.add(key);
  }
  return false;
}

async function save() {
  if (saving.value || !props.contract) {
    return;
  }
  if (duplicateDeclaration()) {
    warning.value = t("dataQuality.planDeclaration.duplicate");
    return;
  }
  saving.value = true;
  error.value = "";
  warning.value = "";
  try {
    const updated = await updateQualityContract({
      targetGuid: props.targetGuid,
      metaType: props.metaType,
      settings:
        props.contract.settings ?? create(QualityContractSettingsSchema),
      draft: buildDeclaration(),
      // The server compares the request's base with published_version and the store
      // compare-and-sets the declaration it read; sending published_version is
      // correct even for rows published before staged_base_version existed.
      stagedBaseVersion: props.contract.publishedVersion,
    });
    notify.success(t("dataQuality.planDeclaration.saved"));
    emit("updated", updated);
    emit("update:open", false);
  } catch (e: unknown) {
    if (e instanceof ConnectError && e.code === Code.Aborted) {
      // The declaration (or the published version it was based on) changed while
      // this dialog was open; retrying with the same stale base would abort again.
      notify.warning(t("dataQuality.planDeclaration.conflict"));
      emit("refresh");
      close();
      return;
    }
    error.value = formatError(e, "dataQuality.planDeclaration.saveFailed");
  } finally {
    saving.value = false;
  }
}
</script>
