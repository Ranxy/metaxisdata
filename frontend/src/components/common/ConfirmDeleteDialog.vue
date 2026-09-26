<template>
  <Dialog
    :open="modelValue"
    @update:open="(open: boolean) => emit('update:modelValue', open)"
  >
    <DialogContent class="max-w-sm">
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
      </DialogHeader>

      <div class="space-y-3 text-center">
        <div
          class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10"
        >
          <Trash2 class="h-6 w-6 text-destructive" />
        </div>
        <DialogDescription class="text-foreground">
          {{ message }}
        </DialogDescription>
        <p
          v-if="itemName"
          class="text-sm text-muted-foreground"
        >
          <strong>{{ itemName }}</strong>
        </p>
      </div>

      <DialogFooter>
        <Button
          variant="outline"
          @click="emit('update:modelValue', false)"
        >
          {{ t("common.cancel") }}
        </Button>
        <Button
          variant="destructive"
          :disabled="loading"
          @click="emit('confirm')"
        >
          {{ t("common.delete") }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { Trash2 } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

/**
 * The one destructive confirmation: a warning icon, the operation's own wording,
 * the object it targets, and Cancel/Delete. It replaces six copies that had
 * drifted apart (three of them had no icon at all), and its own loading state
 * keeps the button disabled while the caller's request is in flight.
 */
interface Props {
  modelValue: boolean;
  title: string;
  message: string;
  /** The object being deleted, shown bold under the message. */
  itemName?: string;
  loading?: boolean;
}

withDefaults(defineProps<Props>(), {
  itemName: "",
  loading: false,
});

const emit = defineEmits<{
  "update:modelValue": [value: boolean];
  confirm: [];
}>();

const { t } = useI18n();
</script>
