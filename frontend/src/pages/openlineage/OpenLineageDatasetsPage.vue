<template>
  <div class="space-y-6">
    <OpenLineageSectionHeader
      :title="t('openlineage.datasets')"
      :description="t('openlineage.datasetsDescription')"
    />

    <Card>
      <CardHeader>
        <CardTitle>{{ t("openlineage.comingSoon") }}</CardTitle>
        <CardDescription>
          {{ t("openlineage.comingSoonDescription") }}
        </CardDescription>
      </CardHeader>
      <CardContent class="space-y-4 text-sm text-muted-foreground">
        <p>
          {{ t("openlineage.browseFromSettings") }}
        </p>
        <Button variant="outline" asChild>
          <RouterLink :to="{ name: 'OpenLineageTasks' }">
            {{ t("openlineage.openJobs") }}
          </RouterLink>
        </Button>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const { t } = useI18n();
</script>