<template>
  <div class="flex w-full min-w-0 items-center gap-1">
    <Button
      :id="id"
      variant="outline"
      class="h-9 min-w-0 flex-1 justify-between px-3 font-normal"
      :title="display"
      :aria-label="label"
      @click="open = true"
    >
      <span
        class="truncate"
        :class="{ 'text-muted-foreground': !modelValue }"
      >
        {{ display }}
      </span>
      <ChevronDown class="ml-1 h-4 w-4 shrink-0 opacity-60" />
    </Button>
    <Button
      v-if="modelValue"
      variant="ghost"
      size="icon"
      class="h-8 w-8 shrink-0"
      :title="t('dataQuality.targetScope.clear')"
      :aria-label="t('dataQuality.targetScope.clear')"
      @click="clear"
    >
      <X class="h-4 w-4" />
    </Button>

    <TargetPicker
      v-model:open="open"
      mode="scope"
      @select-scope="apply"
    />
  </div>
</template>

<script setup lang="ts">
import { ChevronDown, X } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import TargetPicker from "@/components/data-quality/TargetPicker.vue";
import { Button } from "@/components/ui/button";
import type { QualityTargetScope } from "@/utils/dataQuality";

/**
 * The rules list's target-scope filter: a button that shows the picked path
 * (instance > database > schema > table) and opens the shared target browser in
 * scope mode. The value is pending until the page's Apply runs, like the other
 * controls in the row; clearing the X only clears the pending value.
 */

const props = withDefaults(
  defineProps<{
    modelValue?: QualityTargetScope;
    id?: string;
  }>(),
  { modelValue: undefined, id: undefined }
);

const emit = defineEmits<{
  "update:modelValue": [scope: QualityTargetScope | undefined];
}>();

const { t } = useI18n();

const open = ref(false);
const label = computed(() => t("dataQuality.targetScope.label"));
const display = computed(
  () => props.modelValue?.label || t("dataQuality.targetScope.placeholder")
);

function apply(scope: QualityTargetScope) {
  emit("update:modelValue", scope);
}

function clear() {
  emit("update:modelValue", undefined);
}
</script>
