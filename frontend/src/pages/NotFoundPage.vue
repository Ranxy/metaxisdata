<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('notFound.title')"
      :description="t('notFound.description')"
    />

    <Card>
      <CardContent class="pt-6">
        <EmptyState
          :icon="FileQuestion"
          :title="t('notFound.tryThese')"
        >
          <Button as-child>
            <RouterLink :to="{ name: 'Home' }">
              <Home class="mr-2 h-4 w-4" />
              {{ t("notFound.backHome") }}
            </RouterLink>
          </Button>
          <Button variant="outline" @click="goBack">
            <ArrowLeft class="mr-2 h-4 w-4" />
            {{ t("notFound.goBack") }}
          </Button>
        </EmptyState>
      </CardContent>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { ArrowLeft, FileQuestion, Home } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { RouterLink, useRouter } from "vue-router";
import EmptyState from "@/components/common/EmptyState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

const { t } = useI18n();
const router = useRouter();

// Back goes where the mistyped link came from; a direct visit has no history to
// return to, so it falls back to the dashboard.
function goBack() {
  if (window.history.length > 1) {
    router.back();
    return;
  }
  router.push({ name: "Home" });
}
</script>
