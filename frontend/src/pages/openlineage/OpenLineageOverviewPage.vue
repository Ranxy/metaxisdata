<template>
  <div class="space-y-6">
    <OpenLineageSectionHeader
      :title="t('openlineage.overview')"
      :description="t('openlineage.overviewDescription')"
    >
      <template #actions>
        <Badge variant="outline">{{ t("openlineage.phaseOneStatus") }}</Badge>
      </template>
    </OpenLineageSectionHeader>

    <Card>
      <CardContent class="p-6">
        <p class="max-w-4xl text-sm text-muted-foreground">
          {{ t("openlineage.overviewSummary") }}
        </p>
      </CardContent>
    </Card>

    <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
      <Card v-for="entry in entries" :key="entry.title" class="h-full">
        <CardHeader>
          <CardTitle class="flex items-center gap-2 text-lg">
            <component :is="entry.icon" class="size-5 text-primary" />
            {{ entry.title }}
          </CardTitle>
          <CardDescription>{{ entry.description }}</CardDescription>
        </CardHeader>
        <CardContent>
          <Button class="w-full justify-between" variant="outline" asChild>
            <RouterLink :to="entry.to">
              {{ entry.cta }}
              <ArrowRight class="size-4" />
            </RouterLink>
          </Button>
        </CardContent>
      </Card>
    </div>
  </div>
</template>

<script setup lang="ts">
import {
  ArrowRight,
  Database,
  Files,
  Network,
  Settings2,
} from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import OpenLineageSectionHeader from "@/components/openlineage/OpenLineageSectionHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const { t } = useI18n();

const entries = computed(() => [
  {
    title: t("openlineage.jobs"),
    description: t("openlineage.jobsDescription"),
    to: { name: "OpenLineageTasks" },
    cta: t("openlineage.openJobs"),
    icon: Network,
  },
  {
    title: t("openlineage.events"),
    description: t("openlineage.eventsDescription"),
    to: { name: "OpenLineageEvents" },
    cta: t("openlineage.openEvents"),
    icon: Files,
  },
  {
    title: t("openlineage.datasets"),
    description: t("openlineage.datasetsDescription"),
    to: { name: "OpenLineageDatasets" },
    cta: t("openlineage.openDatasets"),
    icon: Database,
  },
  {
    title: t("openlineage.ingestionSettings"),
    description: t("openlineage.browseFromSettings"),
    to: { name: "OpenLineageSettings" },
    cta: t("openlineage.openSettings"),
    icon: Settings2,
  },
]);
</script>