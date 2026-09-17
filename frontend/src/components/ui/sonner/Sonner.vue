<script setup lang="ts">
import { Toaster as Sonner, type ToasterProps } from "vue-sonner";

const props = defineProps<ToasterProps>();

/**
 * Sonner's stylesheet still owns positioning, stacking and the enter/leave
 * animations; these classes reskin the toast surface with the app's design
 * tokens instead of sonner's hardcoded greys.
 *
 * The `!` important modifier is deliberate. Sonner styles the toast through
 * `[data-sonner-toast][data-styled='true']` (specificity 0,2,0) and its children
 * through `[data-sonner-toast][data-styled='true'] [data-icon]` (0,3,0). Plain
 * utilities lose those comparisons regardless of stylesheet order, and Tailwind
 * v4 lowers `group-[...]` variants to `:is(:where(...))`, which does not outrank
 * them either.
 */
const toastClasses = {
  toast:
    "items-start! rounded-lg! border-border! bg-popover! p-4! gap-3! text-sm! text-popover-foreground! shadow-lg!",
  title: "font-medium!",
  description: "text-muted-foreground!",
  // Sonner sizes the icon slot at 16px while the glyph is 20px, and its start
  // margin (-3px) is tuned for its own 6px gap rather than the 12px used here.
  icon: "mx-0! size-5! shrink-0!",
  content: "gap-1!",
  closeButton: "border-border! bg-popover! text-muted-foreground!",
  actionButton: "rounded-md! bg-primary! text-primary-foreground!",
  cancelButton: "rounded-md! bg-muted! text-muted-foreground!",
  // Sonner only colours the icon by status under `richColors`; on a neutral
  // surface the colour is what distinguishes success from failure, so set it
  // per type on the icon slot.
  //
  // These are palette steps rather than `text-destructive`/`text-primary`: the
  // semantic tokens are surface colours, and `--destructive` is a deep maroon in
  // dark mode that all but disappears on the toast. The 500 steps read on both
  // surfaces, and match the palette already used by the Badge variants.
  success: "[&_[data-icon]]:text-green-500!",
  error: "[&_[data-icon]]:text-red-500!",
  warning: "[&_[data-icon]]:text-amber-500!",
  info: "[&_[data-icon]]:text-sky-500!",
};
</script>

<template>
  <Sonner
    class="toaster group"
    v-bind="props"
    :toast-options="{ classes: toastClasses }"
  />
</template>
