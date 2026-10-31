<template>
  <div class="space-y-2">
    <Label for="dq-config-sql">
      {{ t("dataQuality.editor.sql") }}
      <span class="text-destructive">*</span>
    </Label>
    <textarea
      id="dq-config-sql"
      v-model="config.sql"
      class="min-h-64 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
      :aria-required="true"
      :aria-invalid="errorFor(fieldErrors, 'sql') ? true : undefined"
      :class="errorFor(fieldErrors, 'sql') ? 'border-destructive' : ''"
      :placeholder="t('dataQuality.editor.sqlPlaceholder')"
    />
    <p
      v-if="errorFor(fieldErrors, 'sql')"
      class="text-sm text-destructive"
    >
      {{ errorFor(fieldErrors, "sql") }}
    </p>
    <p
      v-else
      class="text-sm text-muted-foreground"
    >
      {{ t("dataQuality.editor.sqlHint") }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { Label } from "@/components/ui/label";
import type { RuleConfigForm } from "@/utils/dataQualityForm";
import { errorFor, type RuleConfigSectionProps } from "./ruleConfigField";

withDefaults(defineProps<RuleConfigSectionProps>(), {
  columnDetails: () => [],
  fieldErrors: () => ({}),
});

const config = defineModel<RuleConfigForm>({ required: true });
const { t } = useI18n();
</script>
