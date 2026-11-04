<template>
  <Card>
    <CardHeader class="space-y-1 border-b">
      <CardTitle class="flex items-center gap-2 text-base">
        <ShieldCheck class="h-4 w-4 shrink-0 text-muted-foreground" />
        {{ t("dataQuality.workspace.planTitle") }}
      </CardTitle>
      <CardDescription>
        {{ t("dataQuality.workspace.planHint") }}
      </CardDescription>
    </CardHeader>
    <CardContent class="space-y-3 pt-4">
      <template v-if="plan">
        <div class="flex flex-wrap items-center gap-2">
          <Badge :variant="plan.state === QualityPlanState.PLANNED ? 'success' : 'neutral'">
            {{ planStateLabel }}
          </Badge>
          <Badge variant="outline">
            {{ t("dataQuality.workspace.planCriticality", { value: criticalityLabel }) }}
          </Badge>
          <span class="text-xs text-muted-foreground">
            {{
              t("dataQuality.workspace.planChecks", {
                checks: plan.checkCount,
                gaps: plan.gapCount,
              })
            }}
          </span>
        </div>

        <p
          v-if="plan.state === QualityPlanState.UNPLANNED"
          class="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground"
        >
          {{ t("dataQuality.workspace.planUnplannedHint") }}
        </p>

        <ul
          v-else
          class="space-y-1.5"
        >
          <li
            v-for="requirement in plan.requirements"
            :key="requirement.property"
            class="flex items-center justify-between gap-2 text-sm"
          >
            <span>{{ propertyLabel(requirement.property) }}</span>
            <Badge :variant="requirement.satisfied ? 'success' : 'warning'">
              {{ requirement.satisfied ? t("dataQuality.workspace.planSatisfied") : t("dataQuality.workspace.planGap") }}
            </Badge>
          </li>
        </ul>
        <p class="text-xs text-muted-foreground">
          {{ t("dataQuality.workspace.planReadOnlyHint") }}
        </p>
      </template>
      <p
        v-else
        class="text-xs text-muted-foreground"
      >
        {{ t("dataQuality.workspace.planEmptyHint") }}
      </p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { ShieldCheck } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  QualityAssetCriticality,
  type QualityContract,
  QualityCoverageProperty,
  QualityPlanState,
} from "@/types/proto-es/v1/data_quality_service_pb";

const props = defineProps<{ contract?: QualityContract }>();
const { t } = useI18n();

const plan = computed(() => props.contract?.plan);

const planStateLabel = computed(() =>
  plan.value?.state === QualityPlanState.PLANNED
    ? t("dataQuality.workspace.planStatePlanned")
    : t("dataQuality.workspace.planStateUnplanned")
);

const criticalityLabel = computed(() => {
  switch (plan.value?.criticality) {
    case QualityAssetCriticality.CRITICAL:
      return t("dataQuality.workspace.planCriticalityCritical");
    case QualityAssetCriticality.IMPORTANT:
      return t("dataQuality.workspace.planCriticalityImportant");
    default:
      return t("dataQuality.workspace.planCriticalityExperimental");
  }
});

function propertyLabel(property: QualityCoverageProperty): string {
  switch (property) {
    case QualityCoverageProperty.VOLUME:
      return t("dataQuality.workspace.planPropertyVolume");
    case QualityCoverageProperty.FRESHNESS:
      return t("dataQuality.workspace.planPropertyFreshness");
    case QualityCoverageProperty.UNIQUENESS:
      return t("dataQuality.workspace.planPropertyUniqueness");
    case QualityCoverageProperty.SCHEMA_STABILITY:
      return t("dataQuality.workspace.planPropertySchemaStability");
    default:
      return t("dataQuality.workspace.planPropertyUnspecified");
  }
}
</script>
