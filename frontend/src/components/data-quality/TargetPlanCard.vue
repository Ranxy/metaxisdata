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
            v-for="row in planRows"
            :key="row.key"
            class="flex items-center justify-between gap-2 text-sm"
          >
            <span>
              {{ t(qualityCoveragePropertyKey(row.property)) }}
              <span
                v-if="row.column"
                class="text-muted-foreground"
              >
                · {{ row.column }}
              </span>
            </span>
            <Badge
              v-if="row.declared"
              variant="secondary"
            >
              {{ t("dataQuality.workspace.planDeclared") }}
            </Badge>
            <Badge
              v-else-if="row.exempted"
              variant="neutral"
            >
              {{ t("dataQuality.workspace.planExempted") }}
            </Badge>
            <Badge
              v-else
              :variant="row.satisfied ? 'success' : 'warning'"
            >
              {{
                row.satisfied
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
      @refresh="emit('refresh')"
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
import { qualityCoveragePropertyKey } from "@/utils/dataQuality";

const props = withDefaults(
  defineProps<{ contract?: QualityContract; canUpdate?: boolean }>(),
  { canUpdate: false }
);
const emit = defineEmits<{
  updated: [contract: QualityContract];
  /** The declaration changed underneath the editor; the page re-reads the contract. */
  refresh: [];
}>();
const { t } = useI18n();

const declarationOpen = ref(false);

const plan = computed(() => props.contract?.plan);
const hasDeclaration = computed(
  () => (props.contract?.stagedPlan?.coverage.length ?? 0) > 0
);

interface PlanRow {
  key: string;
  property: QualityCoverageProperty;
  column: string;
  satisfied: boolean;
  exempted: boolean;
  declared: boolean;
}

/**
 * The rows the card renders: the derived requirements plus any declared coverage
 * the derived list does not contain. A declared promise that no derived requirement
 * lists still shows up, so the "Declared" badge always names what it points at.
 */
const planRows = computed<PlanRow[]>(() => {
  const rows: PlanRow[] = [];
  const seen = new Set<string>();
  for (const requirement of plan.value?.requirements ?? []) {
    const key = `${requirement.property}:${requirement.column}`;
    seen.add(key);
    rows.push({
      key,
      property: requirement.property,
      column: requirement.column,
      satisfied: requirement.satisfied,
      exempted: requirement.exempted,
      declared: false,
    });
  }
  for (const coverage of props.contract?.stagedPlan?.coverage ?? []) {
    const key = `${coverage.property}:${coverage.column}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    rows.push({
      key,
      property: coverage.property,
      column: coverage.column,
      satisfied: false,
      exempted: false,
      declared: true,
    });
  }
  return rows;
});

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
</script>
