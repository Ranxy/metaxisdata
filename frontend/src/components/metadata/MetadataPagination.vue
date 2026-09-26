<template>
  <div class="flex justify-center">
    <Button
      variant="secondary"
      :disabled="!hasNext || isLoading"
      @click="$emit('load-more')"
    >
      <Loader2
        v-if="isLoading"
        class="mr-2 h-4 w-4 animate-spin"
      />
      {{ t("metadataBrowser.loadMore") }}
    </Button>
  </div>
</template>

<script setup lang="ts">
import { Loader2 } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";

defineProps<{
  hasNext: boolean;
  isLoading: boolean;
}>();

defineEmits<{
  "load-more": [];
}>();

const { t } = useI18n();
</script>
