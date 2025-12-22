<template>
  <div class="w-full space-y-2.5">
    <Label
      v-if="label"
      :for="inputId"
    >
      {{ label }}
      <span
        v-if="required"
        class="text-destructive"
      >*</span>
    </Label>
    <div class="relative">
      <Input
        :id="inputId"
        :type="type"
        :model-value="modelValue"
        :placeholder="placeholder"
        :disabled="disabled"
        :readonly="readonly"
        :class="[
          error ? 'border-destructive focus-visible:ring-destructive' : '',
          props.class
        ]"
        @update:model-value="handleInput"
        @blur="$emit('blur', $event)"
        @focus="$emit('focus', $event)"
      />
      <slot name="suffix" />
    </div>
    <p
      v-if="error"
      class="text-sm text-destructive"
    >
      {{ error }}
    </p>
    <p
      v-else-if="hint"
      class="text-sm text-muted-foreground"
    >
      {{ hint }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, type HTMLAttributes } from "vue";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

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
  blur: [event: FocusEvent];
  focus: [event: FocusEvent];
}>();

const inputId = computed(
  () => props.id || `input-${Math.random().toString(36).substring(2, 9)}`
);

function handleInput(value: string | number) {
  emit("update:modelValue", String(value));
}
</script>
