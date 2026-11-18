<template>
  <Card>
    <CardHeader>
      <CardTitle>{{ t("qualityAgents.title") }}</CardTitle>
      <CardDescription>{{ t("qualityAgents.description") }}</CardDescription>
    </CardHeader>
    <CardContent class="pt-2">
      <Button
        variant="outline"
        as-child
      >
        <RouterLink :to="{ name: 'QualityAgentsSettings' }">
          {{ t("qualityAgents.openSettings") }}
        </RouterLink>
      </Button>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
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
