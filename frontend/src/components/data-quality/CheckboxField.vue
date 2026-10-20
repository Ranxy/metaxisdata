<template>
  <div class="flex items-start gap-2 pt-1.5">
    <Checkbox
      :id="fieldId"
      :checked="modelValue"
      @update:checked="(value: boolean | 'indeterminate') => emit('update:modelValue', value === true)"
    />
    <Label
      :for="fieldId"
      class="cursor-pointer text-sm font-normal leading-5"
    >
      {{ label }}
    </Label>
  </div>
</template>

<script setup lang="ts">
import { computed, useId } from "vue";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";

/** A checkbox with its label as one clickable row; the label is the hit target. */
const props = withDefaults(
  defineProps<{
    modelValue?: boolean;
    label: string;
    id?: string;
  }>(),
  { modelValue: false, id: "" }
);

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
}>();

const generatedId = useId();
const fieldId = computed(() => props.id || `checkbox-${generatedId}`);
</script>
