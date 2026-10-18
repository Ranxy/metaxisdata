<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('connectedApps.title')"
      :description="t('connectedApps.selfDescription')"
    >
      <template #actions>
        <Button
          variant="outline"
          :disabled="section?.isLoading"
          @click="section?.refresh()"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': section?.isLoading }"
          />
          {{ t("connectedApps.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <ConnectedAppsSection
      ref="section"
      scope="self"
    />
  </div>
</template>

<script setup lang="ts">
import { RefreshCcw } from "lucide-vue-next";
import { useTemplateRef } from "vue";
import { useI18n } from "vue-i18n";
import ConnectedAppsSection from "@/components/connected-app/ConnectedAppsSection.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Button } from "@/components/ui/button";

interface SectionExposed {
  refresh: () => Promise<void>;
  isLoading: boolean;
}

const { t } = useI18n();
const section = useTemplateRef<SectionExposed>("section");
</script>
