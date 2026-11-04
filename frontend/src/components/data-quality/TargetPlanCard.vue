<template>
  <Card>
    <CardHeader class="space-y-1 border-b">
      <div class="flex items-center justify-between gap-2">
        <CardTitle class="flex items-center gap-2 text-base">
          <ShieldCheck class="h-4 w-4 shrink-0 text-muted-foreground" />
          {{ t("dataQuality.workspace.planTitle") }}
        </CardTitle>
        <Button
          v-if="canUpdate && contract"
          variant="ghost"
          size="sm"
          @click="declarationOpen = true"
        >
          <Pencil class="mr-1 h-3.5 w-3.5" />
          {{ t("dataQuality.workspace.planEdit") }}
        </Button>
      </div>
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
          <Badge :variant="hasDeclaration ? 'secondary' : 'neutral'">
            {{
              hasDeclaration
                ? t("dataQuality.workspace.planDeclared")
                : t("dataQuality.workspace.planDerived")
            }}
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
            :key="`${requirement.property}:${requirement.column}`"
            class="flex items-center justify-between gap-2 text-sm"
          >
            <span>
              {{ propertyLabel(requirement.property) }}
              <span
                v-if="requirement.column"
                class="text-muted-foreground"
              >
                · {{ requirement.column }}
              </span>
            </span>
            <Badge
              v-if="requirement.exempted"
              variant="neutral"
            >
              {{ t("dataQuality.workspace.planExempted") }}
            </Badge>
            <Badge
              v-else
              :variant="requirement.satisfied ? 'success' : 'warning'"
            >
              {{
                requirement.satisfied
                  ? t("dataQuality.workspace.planSatisfied")
                  : t("dataQuality.workspace.planGap")
              }}
            </Badge>
          </li>
        </ul>
        <p class="text-xs text-muted-foreground">
          {{
            hasDeclaration
              ? t("dataQuality.workspace.planDeclaredHint")
              : t("dataQuality.workspace.planDerivedHint")
          }}
        </p>
      </template>
      <p
        v-else
        class="text-xs text-muted-foreground"
      >
        {{ t("dataQuality.workspace.planEmptyHint") }}
      </p>
    </CardContent>

    <TargetPlanDeclarationDialog
      v-if="contract"
      v-model:open="declarationOpen"
      :target-guid="contract.target?.metaGuid ?? ''"
      :meta-type="contract.target?.metaType ?? MetaType.UNSPECIFIED"
      :contract="contract"
      @updated="emit('updated', $event)"
    />
  </Card>
</template>

<script setup lang="ts">
import { Pencil, ShieldCheck } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";
import TargetPlanDeclarationDialog from "@/components/data-quality/TargetPlanDeclarationDialog.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { MetaType } from "@/types/proto-es/v1/database_service_pb";

const props = withDefaults(
  defineProps<{ contract?: QualityContract; canUpdate?: boolean }>(),
  { canUpdate: false }
);
const emit = defineEmits<{ updated: [contract: QualityContract] }>();
const { t } = useI18n();

const declarationOpen = ref(false);

const plan = computed(() => props.contract?.plan);
const hasDeclaration = computed(
  () => (props.contract?.stagedPlan?.coverage.length ?? 0) > 0
);

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
    case QualityCoverageProperty.NULL_RATE:
      return t("dataQuality.workspace.planPropertyNullRate");
    case QualityCoverageProperty.ACCEPTED_VALUES:
      return t("dataQuality.workspace.planPropertyAcceptedValues");
    case QualityCoverageProperty.FORMAT:
      return t("dataQuality.workspace.planPropertyFormat");
    case QualityCoverageProperty.RANGE:
      return t("dataQuality.workspace.planPropertyRange");
    case QualityCoverageProperty.UNIQUENESS:
      return t("dataQuality.workspace.planPropertyUniqueness");
    case QualityCoverageProperty.SCHEMA_STABILITY:
      return t("dataQuality.workspace.planPropertySchemaStability");
    default:
      return t("dataQuality.workspace.planPropertyUnspecified");
  }
}
</script>
