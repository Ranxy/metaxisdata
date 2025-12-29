<template>
  <div
    v-if="text"
    class="flex items-center gap-1"
  >
    <span
      ref="textRef"
      class="truncate"
      :class="textClass"
    >
      {{ text }}
    </span>
    <Popover v-if="isTruncated" v-model:open="popoverOpen">
      <PopoverTrigger asChild>
        <button
          type="button"
          class="flex-shrink-0 text-primary hover:text-primary/80 p-0.5"
          :title="t('common.viewFullContent')"
          @click.stop
        >
          <Expand class="h-3.5 w-3.5" />
        </button>
      </PopoverTrigger>
      <PopoverContent class="w-auto max-w-2xl">
        <div class="grid gap-2">
          <div class="grid gap-0.5">
            <div class="text-sm font-medium">
              {{ dialogTitle || t("common.details") }}
            </div>
            <div v-if="itemName" class="text-sm text-muted-foreground">
              {{ itemName }}
            </div>
          </div>
          <div class="max-h-96 overflow-auto">
            <p class="text-sm whitespace-pre-wrap break-words">{{ text }}</p>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  </div>
  <span
    v-else
    :class="textClass"
  >-</span>
</template>

<script setup lang="ts">
import { useDebounceFn } from "@vueuse/core";
import { Expand } from "lucide-vue-next";
import { nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

const props = defineProps<{
  text: string | undefined;
  itemName?: string;
  dialogTitle?: string;
  textClass?: string;
}>();

const { t } = useI18n();

const textRef = ref<HTMLElement | null>(null);
const isTruncated = ref(false);
const popoverOpen = ref(false);

function checkTruncation() {
  if (textRef.value) {
    isTruncated.value = textRef.value.scrollWidth > textRef.value.clientWidth;
  }
}

const debouncedCheckTruncation = useDebounceFn(checkTruncation, 100);

onMounted(() => {
  checkTruncation();
  window.addEventListener("resize", debouncedCheckTruncation);
});

onUnmounted(() => {
  window.removeEventListener("resize", debouncedCheckTruncation);
});

watch(
  () => props.text,
  () => {
    nextTick(checkTruncation);
  }
);

watch(isTruncated, (value) => {
  if (!value) {
    popoverOpen.value = false;
  }
});
</script>
