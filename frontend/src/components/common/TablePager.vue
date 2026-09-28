<template>
  <div class="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
    <div class="flex items-center gap-2">
      <Label
        :for="sizeId"
        class="whitespace-nowrap text-sm text-muted-foreground"
      >
        {{ t("common.rowsPerPage") }}
      </Label>
      <Select
        :model-value="String(pageSize)"
        @update:model-value="onPageSizeChange"
      >
        <SelectTrigger
          :id="sizeId"
          class="h-9 w-[4.75rem]"
          :aria-label="t('common.rowsPerPage')"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            v-for="size in PAGE_SIZES"
            :key="size"
            :value="String(size)"
          >
            {{ size }}
          </SelectItem>
        </SelectContent>
      </Select>
    </div>

    <div class="flex items-center gap-2">
      <Button
        variant="outline"
        :disabled="!hasPrevious || disabled"
        @click="emit('previous')"
      >
        {{ t("common.previous") }}
      </Button>
      <Button
        variant="outline"
        :disabled="!hasNext || disabled"
        @click="emit('next')"
      >
        {{ t("common.next") }}
      </Button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useId } from "vue";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

/**
 * The footer a server-paged table carries: how many rows to ask for, and the two
 * cursor steps. Audit logs, manual SQL and the databases list each spelled this
 * out by hand; the OpenLineage index pages would have been three more copies.
 */
const PAGE_SIZES = [20, 50, 100];

withDefaults(
  defineProps<{
    pageSize: number;
    hasPrevious?: boolean;
    hasNext?: boolean;
    /** A request is in flight, so neither cursor steps through a page yet. */
    disabled?: boolean;
  }>(),
  { hasPrevious: false, hasNext: false, disabled: false }
);

const emit = defineEmits<{
  "update:pageSize": [value: number];
  previous: [];
  next: [];
}>();

const { t } = useI18n();
const sizeId = useId();

function onPageSizeChange(value: unknown) {
  const size = Number(value);
  if (Number.isFinite(size) && size > 0) {
    emit("update:pageSize", size);
  }
}
</script>
