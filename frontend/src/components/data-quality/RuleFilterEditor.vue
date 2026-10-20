<template>
  <div class="space-y-3">
    <div class="flex items-start justify-between gap-3">
      <div class="space-y-1">
        <Label>{{ t("dataQuality.editor.filter") }}</Label>
        <p class="text-sm text-muted-foreground">
          {{ t("dataQuality.editor.filterHint") }}
        </p>
      </div>
      <Button
        variant="outline"
        size="sm"
        :disabled="clauses.length >= MAX_FILTER_CLAUSES"
        @click="addClause"
      >
        <Plus class="mr-1 h-4 w-4" />
        {{ t("dataQuality.editor.addFilter") }}
      </Button>
    </div>

    <p
      v-if="clauses.length === 0"
      class="rounded-md border border-dashed p-3 text-sm text-muted-foreground"
    >
      {{ t("dataQuality.editor.noFilters") }}
    </p>

    <div
      v-for="(clause, index) in clauses"
      :key="index"
      class="grid items-start gap-3 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_10rem_minmax(0,1fr)_auto]"
    >
      <ColumnSelect
        :model-value="clause.column"
        :columns="columns"
        :aria-label="t('dataQuality.editor.filterColumn')"
        :placeholder="t('dataQuality.editor.filterColumn')"
        @update:model-value="(value) => (clause.column = String(value))"
      />
      <Select
        :model-value="String(clause.op)"
        @update:model-value="(value: unknown) => setOperator(clause, value)"
      >
        <SelectTrigger :aria-label="t('dataQuality.editor.filterOperator')">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="operator in FILTER_OPERATORS"
            :key="operator"
            :value="String(operator)"
          >
            {{ t(qualityFilterOperatorKey(operator)) }}
          </SelectItem>
        </SelectContent>
      </Select>
      <Input
        v-model="clause.values"
        :disabled="!filterOperatorNeedsValues(clause.op)"
        :aria-label="t('dataQuality.editor.filterValues')"
        :placeholder="filterOperatorNeedsValues(clause.op) ? t('dataQuality.editor.filterValues') : t('dataQuality.editor.filterNoValues')"
      />
      <Button
        variant="ghost"
        size="icon"
        class="h-9 w-9 text-muted-foreground hover:text-destructive"
        :aria-label="t('dataQuality.editor.removeFilter')"
        :title="t('dataQuality.editor.removeFilter')"
        @click="removeClause(index)"
      >
        <Trash2 class="h-4 w-4" />
      </Button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Plus, Trash2 } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { QualityFilterOperator } from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityFilterOperatorKey } from "@/utils/dataQuality";
import {
  emptyFilterClause,
  type FilterClauseForm,
  filterOperatorNeedsValues,
  MAX_FILTER_CLAUSES,
  type TargetColumnDetail,
} from "@/utils/dataQualityForm";

/**
 * The rule's AND pre-filter: a list of column/operator/value clauses. The parent
 * owns the array; this component only appends and removes, so the config message
 * is built at one place (`buildFilter`).
 */

const clauses = defineModel<FilterClauseForm[]>({ required: true });

defineProps<{
  /** The target's columns; the picker offers them by name and type. */
  columns: TargetColumnDetail[];
}>();

const { t } = useI18n();

const FILTER_OPERATORS = [
  QualityFilterOperator.EQ,
  QualityFilterOperator.NE,
  QualityFilterOperator.LT,
  QualityFilterOperator.LTE,
  QualityFilterOperator.GT,
  QualityFilterOperator.GTE,
  QualityFilterOperator.IN,
  QualityFilterOperator.NOT_IN,
  QualityFilterOperator.IS_NULL,
  QualityFilterOperator.IS_NOT_NULL,
];

function setOperator(clause: FilterClauseForm, value: unknown) {
  clause.op = Number(value) as QualityFilterOperator;
}

function addClause() {
  if (clauses.value.length >= MAX_FILTER_CLAUSES) {
    return;
  }
  clauses.value = [...clauses.value, emptyFilterClause()];
}

function removeClause(index: number) {
  clauses.value = clauses.value.filter((_, position) => position !== index);
}
</script>
