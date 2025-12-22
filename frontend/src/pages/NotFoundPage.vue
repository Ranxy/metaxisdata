<template>
  <div class="min-h-screen flex items-center justify-center py-12 px-4">
    <div class="text-center">
      <!-- 404 Illustration -->
      <div class="mb-8">
        <div class="text-9xl font-bold text-muted-foreground/20">
          404
        </div>
      </div>

      <!-- Message -->
      <h1 class="text-2xl font-bold mb-2">
        {{ t("notFound.title") }}
      </h1>
      <p class="text-muted-foreground mb-8">
        {{ t("notFound.description") }}
      </p>

      <!-- Back to Home Button -->
      <Button
        as-child
        size="lg"
      >
        <router-link to="/">
          <Home class="w-5 h-5 mr-2" />
          {{ t("notFound.backHome") }}
        </router-link>
      </Button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Home } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { Button } from "@/components/ui/button";

const { t } = useI18n();
</script>
