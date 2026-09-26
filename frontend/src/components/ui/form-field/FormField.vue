<template>
  <div class="w-full space-y-2.5">
    <Label
      v-if="label"
      :for="fieldId"
    >
      {{ label }}
      <span
        v-if="required"
        class="text-destructive"
      >*</span>
    </Label>
    <div class="relative">
      <Input
        :id="fieldId"
        v-bind="$attrs"
        :type="type"
        :model-value="modelValue"
        :placeholder="placeholder"
        :disabled="disabled"
        :readonly="readonly"
        :aria-required="required ? true : undefined"
        :aria-invalid="error ? true : undefined"
        :aria-describedby="describedBy"
        :class="[
          error ? 'border-destructive focus-visible:ring-destructive' : '',
          props.class,
        ]"
        @update:model-value="handleInput"
      />
      <slot name="suffix" />
    </div>
    <p
      v-if="error"
      :id="errorId"
      class="text-sm text-destructive"
    >
      {{ error }}
    </p>
    <p
      v-else-if="hint"
      :id="hintId"
      class="text-sm text-muted-foreground"
    >
      {{ hint }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, type HTMLAttributes, useId } from "vue";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

/**
 * A label, an input and the message under it.
 *
 * The generated id is stable for the component's lifetime (it used to be a fresh
 * `Math.random()` per render, which broke the label association), and the
 * message is wired through `aria-describedby`, with `aria-invalid` set when the
 * field is in error.
 */
defineOptions({ inheritAttrs: false });

interface Props {
  modelValue?: string | number;
  type?: string;
  label?: string;
  placeholder?: string;
  disabled?: boolean;
  readonly?: boolean;
  required?: boolean;
  error?: string;
  hint?: string;
  id?: string;
  class?: HTMLAttributes["class"];
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: "",
  type: "text",
  label: "",
  placeholder: "",
  disabled: false,
  readonly: false,
  required: false,
  error: "",
  hint: "",
  id: "",
  class: "",
});

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

const generatedId = useId();
const fieldId = computed(() => props.id || `field-${generatedId}`);
const errorId = computed(() => `${fieldId.value}-error`);
const hintId = computed(() => `${fieldId.value}-hint`);
const describedBy = computed(() => {
  if (props.error) return errorId.value;
  if (props.hint) return hintId.value;
  return undefined;
});

// Numeric inputs keep the string form they have always had, so a `type="number"`
// field's model stays a string.
function handleInput(value: string | number) {
  emit("update:modelValue", String(value));
}
</script>
