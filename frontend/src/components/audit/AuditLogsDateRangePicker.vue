<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <Button
        variant="outline"
        class="min-h-11 min-w-[18rem] justify-between gap-3 px-3 text-left font-normal shadow-sm"
      >
        <span class="flex min-w-0 items-center gap-2">
          <CalendarRange class="h-4 w-4 shrink-0 text-muted-foreground" />
          <span
            class="truncate"
            :class="chip ? 'text-foreground' : 'text-muted-foreground'"
          >
            {{ chip || t("auditLogs.selectDateRange") }}
          </span>
        </span>
      </Button>
    </PopoverTrigger>
    <PopoverContent
      align="end"
      class="w-auto p-0"
    >
      <div class="space-y-4 p-4">
        <div class="space-y-1">
          <div class="text-sm font-medium">{{ t("auditLogs.created") }}</div>
          <div class="text-xs text-muted-foreground">
            {{ draftChip || t("auditLogs.selectDateRangeHint") }}
          </div>
        </div>

        <RangeCalendarRoot
          v-model="draft"
          :locale="locale"
          :number-of-months="2"
          fixed-weeks
          initial-focus
          class="rounded-md border p-3"
        >
          <template #default="{ grid, weekDays }">
            <RangeCalendarHeader class="mb-4 flex items-center justify-between">
              <RangeCalendarPrev class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background transition-colors hover:bg-accent hover:text-accent-foreground">
                <ChevronLeft class="h-4 w-4" />
              </RangeCalendarPrev>
              <RangeCalendarHeading class="text-sm font-medium" />
              <RangeCalendarNext class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background transition-colors hover:bg-accent hover:text-accent-foreground">
                <ChevronRight class="h-4 w-4" />
              </RangeCalendarNext>
            </RangeCalendarHeader>

            <div class="flex flex-col gap-4 sm:flex-row sm:gap-6">
              <RangeCalendarGrid
                v-for="month in grid"
                :key="month.value.toString()"
                class="select-none space-y-1"
              >
                <RangeCalendarGridHead>
                  <RangeCalendarGridRow class="mb-1 flex">
                    <RangeCalendarHeadCell
                      v-for="day in weekDays"
                      :key="day"
                      class="w-9 rounded-md text-xs font-normal text-muted-foreground"
                    >
                      {{ day }}
                    </RangeCalendarHeadCell>
                  </RangeCalendarGridRow>
                </RangeCalendarGridHead>
                <RangeCalendarGridBody class="space-y-1">
                  <RangeCalendarGridRow
                    v-for="(weekDates, weekIndex) in month.rows"
                    :key="`${month.value.toString()}-${weekIndex}`"
                    class="flex"
                  >
                    <RangeCalendarCell
                      v-for="weekDate in weekDates"
                      :key="weekDate.toString()"
                      :date="weekDate"
                      class="relative h-9 w-9 p-0 text-center text-sm focus-within:relative focus-within:z-20 [&:has([data-highlighted])]:bg-accent/50 [&:has([data-selection-end])]:rounded-r-md [&:has([data-selection-start])]:rounded-l-md"
                    >
                      <RangeCalendarCellTrigger
                        :day="weekDate"
                        :month="month.value"
                        class="flex h-9 w-9 items-center justify-center rounded-md border border-transparent bg-transparent p-0 text-sm font-normal outline-none transition-colors hover:bg-accent hover:text-accent-foreground focus:ring-2 focus:ring-ring focus:ring-offset-2 data-[disabled]:pointer-events-none data-[disabled]:opacity-30 data-[highlighted]:bg-accent/80 data-[outside-view]:text-muted-foreground/30 data-[selected]:bg-primary data-[selected]:text-primary-foreground data-[selection-end]:bg-primary data-[selection-end]:text-primary-foreground data-[selection-start]:bg-primary data-[selection-start]:text-primary-foreground data-[today]:border-border"
                      />
                    </RangeCalendarCell>
                  </RangeCalendarGridRow>
                </RangeCalendarGridBody>
              </RangeCalendarGrid>
            </div>
          </template>
        </RangeCalendarRoot>

        <div class="flex items-center justify-between border-t pt-3">
          <Button
            variant="ghost"
            size="sm"
            @click="draft = emptyDateRange()"
          >
            {{ t("auditLogs.clearDateRange") }}
          </Button>
          <div class="flex items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              @click="open = false"
            >
              {{ t("common.cancel") }}
            </Button>
            <Button
              size="sm"
              @click="apply"
            >
              {{ t("auditLogs.applyDateRange") }}
            </Button>
          </div>
        </div>
      </div>
    </PopoverContent>
  </Popover>
</template>

<script setup lang="ts">
import { CalendarRange, ChevronLeft, ChevronRight } from "lucide-vue-next";
import {
  type DateRange,
  RangeCalendarCell,
  RangeCalendarCellTrigger,
  RangeCalendarGrid,
  RangeCalendarGridBody,
  RangeCalendarGridHead,
  RangeCalendarGridRow,
  RangeCalendarHeadCell,
  RangeCalendarHeader,
  RangeCalendarHeading,
  RangeCalendarNext,
  RangeCalendarPrev,
  RangeCalendarRoot,
} from "radix-vue";
import { computed, ref, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  cloneDateRange,
  emptyDateRange,
  formatDateRangeChip,
  isSameDateRange,
} from "@/utils/dateRange";

const props = defineProps<{
  /** The applied range. The picker keeps its edits in a draft until Apply. */
  modelValue: DateRange;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: DateRange];
  apply: [];
}>();

const { t, locale } = useI18n();

const open = ref(false);
// shallowRef: Vue's deep unwrap rewrites the calendar classes into structural
// look-alikes that radix's own DateRange rejects.
const draft = shallowRef<DateRange>(cloneDateRange(props.modelValue));

const chip = computed(() =>
  formatDateRangeChip(props.modelValue, locale.value)
);
const draftChip = computed(() =>
  formatDateRangeChip(draft.value, locale.value)
);

// Reopening starts from what is applied, not from the abandoned draft.
watch(open, (isOpen) => {
  if (isOpen) {
    draft.value = cloneDateRange(props.modelValue);
  }
});

function apply() {
  open.value = false;
  if (isSameDateRange(props.modelValue, draft.value)) {
    return;
  }
  emit("update:modelValue", cloneDateRange(draft.value));
  emit("apply");
}
</script>
