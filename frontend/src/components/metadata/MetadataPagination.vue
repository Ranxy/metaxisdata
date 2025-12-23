<template>
  <div class="flex justify-center">
    <AppButton
      variant="secondary"
      size="md"
      :loading="isLoading"
      :disabled="!hasNext || isLoading"
      @click="$emit('load-more')"
    >
      {{ t("metadataBrowser.loadMore") }}
    </AppButton>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import AppButton from "@/components/common/AppButton.vue";

defineProps<{
  hasNext: boolean;
  isLoading: boolean;
}>();

defineEmits<{
  "load-more": [];
}>();

const { t } = useI18n();
</script>
