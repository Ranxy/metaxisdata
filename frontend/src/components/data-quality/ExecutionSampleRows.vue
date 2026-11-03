<template>
  <!-- One block per sampled row, not a table: the columns are data-dependent,
       and a wide table is what the layout audit rejects. A row is numbered
       because two failures on the same column can otherwise look like the same
       row repeated, and a NULL or an empty string is printed as such: a blank
       cell would read as a rendering defect. A sample recorded before the null
       marker existed cannot say which of the two it was, so it gets the neutral
       form. -->
  <div class="space-y-2">
    <div
      v-for="(sample, index) in samples"
      :key="index"
      class="rounded-md border p-3 text-xs"
    >
      <div class="mb-1 font-mono text-muted-foreground">
        {{ t("dataQuality.sampleNumber", { index: startIndex + index + 1 }) }}
      </div>
      <div
        v-for="cell in sample.columns"
        :key="cell.column"
        class="flex gap-2 py-0.5"
      >
        <span
          class="w-40 shrink-0 truncate font-mono text-muted-foreground"
          :title="cell.column"
        >
          {{ cell.column }}
        </span>
        <span
          v-if="cell.isNull === true"
          class="min-w-0 font-mono text-muted-foreground italic"
        >
          {{ t("dataQuality.sampleNull") }}
        </span>
        <span
          v-else-if="cell.value !== ''"
          class="min-w-0 break-all"
        >{{ cell.value
          }}<span
            v-if="cell.masked"
            class="ml-1 font-mono text-muted-foreground"
          >{{ t("dataQuality.sampleMasked") }}</span></span>
        <span
          v-else-if="cell.isNull === false"
          class="min-w-0 font-mono text-muted-foreground italic"
        >
          {{ t("dataQuality.sampleEmpty") }}
        </span>
        <span
          v-else
          class="min-w-0 font-mono text-muted-foreground italic"
          :title="t('dataQuality.sampleLegacyHint')"
        >
          {{ t("dataQuality.sampleAmbiguous") }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import type { FailureSample } from "@/types/proto-es/v1/data_quality_service_pb";

/**
 * The sampled rows of one execution, rendered as compact blocks. The failed rows
 * card and the passing rows card share it so the two populations cannot drift
 * into different renderings.
 */

withDefaults(
  defineProps<{
    samples: FailureSample[];
    /** How many rows precede this list, so the numbering continues. */
    startIndex?: number;
  }>(),
  { startIndex: 0 }
);

const { t } = useI18n();
</script>
