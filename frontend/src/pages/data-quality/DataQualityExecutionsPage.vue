<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQuality.executionsTitle')"
      :description="t('dataQuality.executionsDescription')"
    >
      <template #actions>
        <!-- The view switch lives in the header actions on purpose: a separate
             row above the card would push the table past this route's
             tableTopY <= 200 budget. -->
        <div
          class="flex items-center gap-0.5 rounded-lg bg-muted p-0.5"
          role="group"
          :aria-label="t('dataQuality.executionsTitle')"
        >
          <button
            v-for="option in viewOptions"
            :key="option.value"
            type="button"
            class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:outline-none"
            :class="
              activeView === option.value
                ? 'bg-card text-foreground shadow-e1'
                : 'text-muted-foreground hover:text-foreground'
            "
            :aria-pressed="activeView === option.value"
            @click="switchView(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
        <Button
          variant="outline"
          :disabled="activeLoading"
          @click="refreshCurrent"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': activeLoading }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <!-- BY TARGET: the directory of targets with their latest scan batch. This
         is the default view; ?view=executions keeps the flat list below. -->
    <Card v-if="!flatView">
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-48 flex-1 flex-col gap-1.5 sm:max-w-sm sm:flex-none">
          <Label
            for="data-quality-target-search"
            class="sr-only"
          >
            {{ t("dataQuality.search") }}
          </Label>
          <Input
            id="data-quality-target-search"
            v-model="targetSearch"
            class="w-full"
            :placeholder="t('dataQuality.executions.targetSearch')"
            @keyup.enter="applyTargetSearch"
          />
        </div>
        <div class="flex min-w-40 flex-col gap-1.5 [&>select]:hidden sm:ml-auto sm:w-44">
          <Label
            for="data-quality-target-sort"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.targets.sort") }}
          </Label>
          <Select v-model="targetSort">
            <SelectTrigger
              id="data-quality-target-sort"
              class="w-full"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="option in targetSortOptions"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <PageState
        :loading="targetsLoading && targets.length === 0"
        :error="targetsError"
        :title="t('dataQuality.targets.loadFailed')"
      >
        <template #error>
          <Button
            variant="outline"
            size="sm"
            @click="resetTargets"
          >
            {{ t("common.refresh") }}
          </Button>
        </template>

        <EmptyState
          v-if="targets.length === 0"
          :icon="Activity"
          :title="
            appliedTargetSearch
              ? t('dataQuality.noFilteredExecutions')
              : t('dataQuality.noExecutions')
          "
          :description="
            appliedTargetSearch ? '' : t('dataQuality.noExecutionsHint')
          "
        >
          <Button
            v-if="appliedTargetSearch"
            variant="outline"
            size="sm"
            @click="clearTargetSearch"
          >
            {{ t("common.filter.clear") }}
          </Button>
          <Button
            v-else
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityTargets' }">
              {{ t("dataQuality.targets.title") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <div v-else>
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.executions.targetColumn") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.executions.latestScan") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.executions.runResult") }}
                </TableHead>
                <TableHead class="hidden lg:table-cell">
                  {{ t("dataQuality.executions.runDuration") }}
                </TableHead>
                <TableHead class="hidden text-right md:table-cell">
                  {{ t("dataQuality.executions.failedChecks") }}
                </TableHead>
                <TableHead class="hidden text-right md:table-cell">
                  {{ t("dataQuality.executions.runCount") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("dataQuality.executions.historyTitle") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <template
                v-for="summary in targets"
                :key="targetGuid(summary)"
              >
                <TableRow :data-target-guid="targetGuid(summary)">
                  <TableCell>
                    <div class="flex max-w-[14rem] min-w-0 flex-wrap items-center gap-2 md:max-w-md">
                      <RouterLink
                        :to="targetRoute(summary)"
                        class="truncate text-left font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                        :title="targetPathLine(summary)"
                      >
                        {{ targetName(summary) }}
                      </RouterLink>
                      <Badge variant="outline">
                        {{ metaTypeSingularLabel(summary.target?.metaType ?? MetaType.UNSPECIFIED, t) }}
                      </Badge>
                      <Badge
                        v-if="summary.orphaned"
                        variant="danger"
                        :title="t('dataQuality.workspace.orphanedHint')"
                      >
                        {{ t("dataQuality.orphaned") }}
                      </Badge>
                    </div>
                    <div
                      class="max-w-[14rem] truncate font-mono text-xs text-muted-foreground md:max-w-md"
                      :title="targetPathLine(summary)"
                    >
                      {{ targetPathLine(summary) }}
                    </div>
                    <!-- Below md the scan columns collapse in here. -->
                    <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                      <template v-if="latestRun(summary)">
                        <span :title="scanTimeTitle(summary)">
                          {{ scanTimeLabel(summary) }}
                        </span>
                        <span aria-hidden="true">·</span>
                        <span>
                          {{ t(qualityExecutionTriggerKey(latestRun(summary)?.trigger ?? QualityExecutionTrigger.UNSPECIFIED)) }}
                        </span>
                      </template>
                      <span v-else>-</span>
                      <Badge
                        v-if="runOutcome(summary) !== QualityStatus.UNSPECIFIED"
                        :variant="qualityStatusVariant(runOutcome(summary))"
                      >
                        {{ t(qualityStatusKey(runOutcome(summary))) }}
                      </Badge>
                      <span
                        v-if="runStateHint(summary)"
                        class="min-w-0"
                      >
                        {{ runStateHint(summary) }}
                      </span>
                      <span class="tabular-nums">
                        {{ t("dataQuality.executions.runCountValue", { count: summary.runCount }) }}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell
                    class="hidden md:table-cell"
                    :title="scanTimeTitle(summary)"
                  >
                    <template v-if="latestRun(summary)">
                      <div class="whitespace-nowrap">
                        {{ scanTimeLabel(summary) }}
                      </div>
                      <div class="mt-1 text-xs text-muted-foreground">
                        {{ t(qualityExecutionTriggerKey(latestRun(summary)?.trigger ?? QualityExecutionTrigger.UNSPECIFIED)) }}
                      </div>
                    </template>
                    <span
                      v-else
                      class="text-sm text-muted-foreground"
                    >
                      -
                    </span>
                  </TableCell>
                  <TableCell
                    class="hidden md:table-cell"
                    :title="runStateHint(summary)"
                  >
                    <template v-if="latestRun(summary)">
                      <Badge
                        v-if="runOutcome(summary) !== QualityStatus.UNSPECIFIED"
                        :variant="qualityStatusVariant(runOutcome(summary))"
                      >
                        {{ t(qualityStatusKey(runOutcome(summary))) }}
                      </Badge>
                      <Badge
                        v-else-if="runState(summary) !== QualityExecutionState.COMPLETED"
                        :variant="qualityExecutionStateVariant(runState(summary))"
                      >
                        {{ t(qualityExecutionStateKey(runState(summary))) }}
                      </Badge>
                      <div
                        v-if="countsSummary(summary)"
                        class="mt-1 text-xs text-muted-foreground tabular-nums"
                      >
                        {{ countsSummary(summary) }}
                      </div>
                      <div
                        v-if="partialChecks(summary) > 0"
                        class="mt-1 text-xs text-muted-foreground tabular-nums"
                      >
                        {{ t("dataQuality.executions.runPartial", { done: runCounts(summary)?.total ?? 0, all: partialChecks(summary) }) }}
                      </div>
                      <div
                        v-if="unfinishedChecks(summary) > 0"
                        class="mt-1 text-xs text-warning tabular-nums"
                      >
                        {{ t("dataQuality.executions.runIncomplete", { count: unfinishedChecks(summary) }) }}
                      </div>
                    </template>
                    <span
                      v-else
                      class="text-sm text-muted-foreground"
                    >
                      -
                    </span>
                  </TableCell>
                  <TableCell class="hidden whitespace-nowrap tabular-nums lg:table-cell">
                    {{ runDurationLabel(summary) }}
                  </TableCell>
                  <TableCell class="hidden text-right tabular-nums md:table-cell">
                    <span :class="{ 'text-destructive': failedChecks(summary) > 0 }">
                      {{ failedChecks(summary) > 0 ? failedChecks(summary) : "-" }}
                    </span>
                  </TableCell>
                  <TableCell class="hidden text-right tabular-nums md:table-cell">
                    {{ t("dataQuality.executions.runCountValue", { count: summary.runCount }) }}
                  </TableCell>
                  <TableCell class="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      class="h-8 w-8"
                      :aria-expanded="isExpanded(summary)"
                      :aria-label="t('dataQuality.executions.historyTitle')"
                      :title="t('dataQuality.executions.historyTitle')"
                      @click="toggleTarget(summary)"
                    >
                      <ChevronDown
                        v-if="isExpanded(summary)"
                        class="h-4 w-4"
                      />
                      <ChevronRight
                        v-else
                        class="h-4 w-4"
                      />
                    </Button>
                  </TableCell>
                </TableRow>
                <TableRow v-if="isExpanded(summary)">
                  <TableCell
                    :colspan="7"
                    class="bg-muted/20 p-0"
                  >
                    <TargetScanHistory
                      :target-guid="targetGuid(summary)"
                      :meta-type="summary.target?.metaType ?? MetaType.UNSPECIFIED"
                      :rule-titles="ruleTitles"
                      :target-rule-count="summary.enabledRuleCount"
                      :expand-run="expandedRun"
                      @update:expand-run="onRunToggle"
                    />
                  </TableCell>
                </TableRow>
              </template>
            </TableBody>
          </Table>

          <div class="px-6 pb-6">
            <TablePager
              v-model:page-size="targetPageSize"
              :has-previous="targetsHasPrevious"
              :has-next="targetsHasNext"
              :disabled="targetsLoading"
              @previous="goPreviousTargets"
              @next="goNextTargets"
            />
          </div>
        </div>
      </PageState>
    </Card>

    <!-- BY EXECUTION: the flat list, unchanged apart from the run filter chip. -->
    <Card v-else>
      <!-- One filter row, applied together rather than per keystroke: a Select
           change does not re-query until Apply. A rule, target or run carried in
           the URL is shown as a removable chip instead, because it came from a
           cross-page link rather than from this row. -->
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-execution-state"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.filterState") }}
          </Label>
          <Select v-model="state">
            <SelectTrigger
              id="data-quality-execution-state"
              class="w-full sm:w-40"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allExecutionStates") }}
              </SelectItem>
              <SelectItem
                v-for="option in stateOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-execution-outcome"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.outcome") }}
          </Label>
          <Select v-model="outcome">
            <SelectTrigger
              id="data-quality-execution-outcome"
              class="w-full sm:w-40"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allOutcomes") }}
              </SelectItem>
              <SelectItem
                v-for="option in outcomeOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex min-w-36 flex-1 flex-col gap-1.5 [&>select]:hidden sm:flex-none">
          <Label
            for="data-quality-execution-trigger"
            class="text-sm font-medium leading-none"
          >
            {{ t("dataQuality.trigger") }}
          </Label>
          <Select v-model="trigger">
            <SelectTrigger
              id="data-quality-execution-trigger"
              class="w-full sm:w-40"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("dataQuality.allTriggers") }}
              </SelectItem>
              <SelectItem
                v-for="option in triggerOptions"
                :key="option.value"
                :value="String(option.value)"
              >
                {{ t(option.key) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <Button
          variant="outline"
          @click="applyFilters"
        >
          {{ t("common.apply") }}
        </Button>

        <div
          v-if="ruleFilter"
          class="flex min-w-0 items-center gap-1 pb-1.5"
        >
          <Badge
            variant="secondary"
            class="max-w-64 truncate"
            :title="ruleFilter"
          >
            {{ t("dataQuality.filteredByRule", { rule: ruleLabel(ruleFilter) }) }}
          </Badge>
          <Button
            variant="ghost"
            size="icon"
            class="h-7 w-7"
            :title="t('dataQuality.clearRuleFilter')"
            :aria-label="t('dataQuality.clearRuleFilter')"
            @click="clearRuleFilter"
          >
            <X class="h-3.5 w-3.5" />
          </Button>
        </div>
        <div
          v-if="targetFilter"
          class="flex min-w-0 items-center gap-1 pb-1.5"
        >
          <Badge
            variant="secondary"
            class="max-w-64 truncate"
            :title="targetFilter"
          >
            {{ t("dataQuality.filteredByTarget", { target: targetFilter }) }}
          </Badge>
          <Button
            variant="ghost"
            size="icon"
            class="h-7 w-7"
            :title="t('dataQuality.clearTargetFilter')"
            :aria-label="t('dataQuality.clearTargetFilter')"
            @click="clearTargetFilter"
          >
            <X class="h-3.5 w-3.5" />
          </Button>
        </div>
        <div
          v-if="runFilter"
          class="flex min-w-0 items-center gap-1 pb-1.5"
        >
          <Badge
            variant="secondary"
            class="max-w-64 truncate"
            :title="runFilter"
          >
            {{ t("dataQuality.filteredByRun", { run: runShortID }) }}
          </Badge>
          <Button
            variant="ghost"
            size="icon"
            class="h-7 w-7"
            :title="t('dataQuality.clearRunFilter')"
            :aria-label="t('dataQuality.clearRunFilter')"
            @click="clearRunFilter"
          >
            <X class="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      <PageState
        :loading="executionsLoading && executions.length === 0"
        :error="executionsError"
      >
        <EmptyState
          v-if="executions.length === 0"
          :icon="Activity"
          :title="
            hasActiveFilters
              ? t('dataQuality.noFilteredExecutions')
              : t('dataQuality.noExecutions')
          "
          :description="
            hasActiveFilters ? '' : t('dataQuality.noExecutionsHint')
          "
        >
          <Button
            v-if="hasActiveFilters"
            variant="outline"
            size="sm"
            @click="clearFilters"
          >
            {{ t("common.filter.clear") }}
          </Button>
          <Button
            v-else-if="canViewRule"
            variant="outline"
            size="sm"
            as-child
          >
            <RouterLink :to="{ name: 'DataQualityChecks' }">
              {{ t("dataQuality.viewRules") }}
            </RouterLink>
          </Button>
        </EmptyState>

        <div v-else>
          <Table class="[&_th]:whitespace-nowrap">
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("dataQuality.execution") }}</TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.rule") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.ruleTarget") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.observed") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
                  {{ t("dataQuality.failedRows") }}
                </TableHead>
                <TableHead class="hidden md:table-cell">
                  {{ t("dataQuality.trigger") }}
                </TableHead>
                <TableHead class="hidden whitespace-nowrap md:table-cell">
                  {{ t("dataQuality.startedAt") }}
                </TableHead>
                <TableHead class="w-12 text-right">
                  <span class="sr-only">{{ t("common.actions") }}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="execution in executions"
                :key="execution.name"
                class="cursor-pointer"
                @click="openExecution(execution)"
              >
                <TableCell>
                  <div class="flex flex-wrap items-center gap-2">
                    <Badge
                      :variant="qualityExecutionStateVariant(execution.state)"
                    >
                      {{ t(qualityExecutionStateKey(execution.state)) }}
                    </Badge>
                    <Badge
                      v-if="execution.outcome !== QualityStatus.UNSPECIFIED"
                      :variant="qualityStatusVariant(execution.outcome)"
                      :title="t('dataQuality.outcome')"
                    >
                      {{ t(qualityStatusKey(execution.outcome)) }}
                    </Badge>
                  </div>
                  <div class="mt-1 font-mono text-xs text-muted-foreground">
                    {{ qualityExecutionID(execution.name) }}
                  </div>
                  <!-- Below md the rule, target, observed, rows, trigger,
                       duration and started columns collapse in here. -->
                  <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground md:hidden">
                    <RouterLink
                      v-if="canViewRule"
                      :to="ruleTargetRoute(execution)"
                      class="min-w-0 truncate hover:underline"
                      @click.stop
                    >
                      {{ ruleLabel(execution.rule) }}
                    </RouterLink>
                    <span
                      v-else
                      class="min-w-0 truncate"
                    >
                      {{ ruleLabel(execution.rule) }}
                    </span>
                    <span aria-hidden="true">·</span>
                    <RouterLink
                      v-if="hasTarget(execution)"
                      :to="executionTargetRoute(execution)"
                      class="min-w-0 truncate hover:underline"
                      :title="targetTitle(execution)"
                      @click.stop
                    >
                      {{ targetPath(execution) }}
                    </RouterLink>
                    <span
                      v-else
                      class="min-w-0 truncate"
                      :title="targetTitle(execution)"
                    >
                      {{ targetPath(execution) }}
                    </span>
                    <span aria-hidden="true">·</span>
                    <span class="tabular-nums">{{ observedLabel(execution) }}</span>
                    <span aria-hidden="true">·</span>
                    <span class="tabular-nums">{{ rowsLabel(execution) }}</span>
                    <span aria-hidden="true">·</span>
                    <span>{{ t(qualityExecutionTriggerKey(execution.trigger)) }}</span>
                    <span aria-hidden="true">·</span>
                    <span>{{ durationLabel(execution) }}</span>
                    <span aria-hidden="true">·</span>
                    <span :title="formatDateTime(execution.startTime, locale)">
                      {{ formatRelativeTime(execution.startTime, locale) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  <RouterLink
                    v-if="canViewRule"
                    :to="ruleTargetRoute(execution)"
                    class="block max-w-[11rem] truncate font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none lg:max-w-[14rem]"
                    :title="ruleLabel(execution.rule)"
                    @click.stop
                  >
                    {{ ruleLabel(execution.rule) }}
                  </RouterLink>
                  <div
                    v-else
                    class="max-w-[11rem] truncate font-medium lg:max-w-[14rem]"
                    :title="ruleLabel(execution.rule)"
                  >
                    {{ ruleLabel(execution.rule) }}
                  </div>
                </TableCell>
                <TableCell class="hidden max-w-[14rem] md:table-cell">
                  <RouterLink
                    v-if="hasTarget(execution)"
                    :to="executionTargetRoute(execution)"
                    class="block truncate text-xs hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                    :title="targetTitle(execution)"
                    @click.stop
                  >
                    {{ targetPath(execution) }}
                  </RouterLink>
                  <div
                    v-else
                    class="truncate text-xs"
                    :title="targetTitle(execution)"
                  >
                    {{ targetPath(execution) }}
                  </div>
                </TableCell>
                <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                  {{ observedLabel(execution) }}
                </TableCell>
                <TableCell class="hidden whitespace-nowrap tabular-nums md:table-cell">
                  {{ rowsLabel(execution) }}
                </TableCell>
                <TableCell class="hidden md:table-cell">
                  {{ t(qualityExecutionTriggerKey(execution.trigger)) }}
                </TableCell>
                <TableCell
                  class="hidden whitespace-nowrap text-muted-foreground md:table-cell"
                  :title="formatDateTime(execution.startTime, locale)"
                >
                  {{ formatRelativeTime(execution.startTime, locale) }}
                  <div class="mt-1 text-xs tabular-nums">
                    {{ t("dataQuality.duration") }} {{ durationLabel(execution) }}
                  </div>
                </TableCell>
                <TableCell
                  class="text-right"
                  @click.stop
                >
                  <Button
                    variant="ghost"
                    size="icon"
                    class="h-8 w-8"
                    :title="t('dataQuality.viewExecution')"
                    :aria-label="t('dataQuality.viewExecution')"
                    @click="openExecution(execution)"
                  >
                    <Eye class="h-4 w-4 text-muted-foreground" />
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

          <!-- The pager footer keeps the card's content inset. -->
          <div class="px-6 pb-6">
            <TablePager
              v-model:page-size="executionsPageSize"
              :has-previous="executionsHasPrevious"
              :has-next="executionsHasNext"
              :disabled="executionsLoading"
              @previous="goPreviousExecutions"
              @next="goNextExecutions"
            />
          </div>
        </div>
      </PageState>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { useDebounceFn } from "@vueuse/core";
import {
  Activity,
  ChevronDown,
  ChevronRight,
  Eye,
  RefreshCcw,
  X,
} from "lucide-vue-next";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  type LocationQueryRaw,
  type RouteLocationRaw,
  RouterLink,
  useRoute,
  useRouter,
} from "vue-router";
import {
  listAllQualityRules,
  listQualityExecutions,
  listQualityTargets,
  qualityExecutionID,
  qualityRuleID,
} from "@/api/data-quality";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import TargetScanHistory from "@/components/data-quality/TargetScanHistory.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { usePolling } from "@/composables/usePolling";
import { partialQualityRunChecks } from "@/lib/qualityRun";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityRuleState,
  QualityStatus,
  type QualityStatusCounts,
  QualityTargetOrder,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  executionDurationSeconds,
  formatObservedValue,
  formatRowCount,
  formatSeconds,
  formatTargetPath,
  isExecutionInProgress,
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityExecutionTriggerKey,
  qualityStatusKey,
  qualityStatusVariant,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { guidToRouteParams } from "@/utils/guid";
import { metaTypeSingularLabel } from "@/utils/metaType";

/**
 * Quality runs, read two ways.
 *
 * By target (default): one row per target with the latest scan batch that
 * touched it — the batch's result, failed checks, duration and how many batches
 * the target has behind it. Expanding a row lazily loads the target's scan
 * history, and each run expands to its checks.
 *
 * By execution (`?view=executions`): the flat per-check list, kept for auditing
 * and for the deep links that name one rule, target or run.
 *
 * The URL carries which view is open and which target/run is expanded, so a
 * refresh, a back/forward step or a shared link lands on the same place.
 */

/** How often a page that still has pending/running executions is reloaded. */
const POLL_INTERVAL_MS = 5000;

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();

const canViewRule = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.rules.get")
);

// ---- view: by target (default) or by execution ----

type ExecutionsView = "targets" | "executions";

const viewOptions = computed<Array<{ value: ExecutionsView; label: string }>>(
  () => [
    { value: "targets", label: t("dataQuality.executions.viewTargets") },
    { value: "executions", label: t("dataQuality.executions.viewExecutions") },
  ]
);

function queryString(value: unknown): string {
  if (Array.isArray(value)) {
    return String(value[0] ?? "");
  }
  return value === undefined || value === null ? "" : String(value);
}

/** `?view=executions` is the flat list; anything else is the target view. */
const viewParam = computed(() => queryString(route.query.view));
/**
 * A run on its own is an execution list; with a target it is one of that
 * target's scans, opened in place. An explicit `view` always wins.
 */
const flatView = computed(() => {
  if (viewParam.value) {
    return viewParam.value === "executions";
  }
  return (
    queryString(route.query.run) !== "" &&
    queryString(route.query.target) === ""
  );
});

const activeView = computed<ExecutionsView>(() =>
  flatView.value ? "executions" : "targets"
);

function setQuery(patch: Record<string, string | undefined>) {
  const next: LocationQueryRaw = { ...route.query };
  for (const [key, value] of Object.entries(patch)) {
    if (value === undefined || value === "") {
      delete next[key];
    } else {
      next[key] = value;
    }
  }
  void router.replace({ query: next });
}

function switchView(view: ExecutionsView) {
  if (view === "executions") {
    setQuery({ view: "executions" });
    return;
  }
  // The target view is the default, so it needs no `view`; the flat-only
  // filters stop applying, and `target` becomes the expanded row.
  setQuery({ view: undefined, rule: undefined, run: undefined });
}

// ---- shared: rule titles ----

const ruleTitles = ref<Record<string, string>>({});

/**
 * Titles for the rules the rows name. Best effort: a caller without rules.list
 * still sees the executions it is allowed to read, with the rule id as label.
 */
async function loadRuleTitles() {
  try {
    // Archived rules are not in the default list, but their historical
    // executions still need a title rather than a raw resource id.
    const [active, archived] = await Promise.all([
      listAllQualityRules(),
      listAllQualityRules({ state: QualityRuleState.ARCHIVED }),
    ]);
    ruleTitles.value = Object.fromEntries(
      [...active, ...archived].map((rule) => [rule.name, rule.title])
    );
  } catch {
    ruleTitles.value = {};
  }
}

// ---- by target ----

const targetsError = ref("");
const targetPageSize = ref(50);
const targetSearch = ref("");
const appliedTargetSearch = ref("");
const targetSort = ref<"lastRun" | "attention">("lastRun");

const targetSortOptions = computed<
  Array<{ value: "lastRun" | "attention"; label: string }>
>(() => [
  { value: "lastRun", label: t("dataQuality.targets.sortLastRun") },
  { value: "attention", label: t("dataQuality.targets.sortAttention") },
]);

const targetOrderBy = computed(() =>
  targetSort.value === "attention"
    ? QualityTargetOrder.ATTENTION
    : QualityTargetOrder.LAST_RUN
);

const {
  items: targets,
  isLoading: targetsLoading,
  hasNext: targetsHasNext,
  hasPrevious: targetsHasPrevious,
  reset: resetTargets,
  refresh: refreshTargets,
  goNext: goNextTargets,
  goPrevious: goPreviousTargets,
} = usePagedFetch<QualityTargetSummary>({
  fetchPage: async (pageToken, signal) => {
    targetsError.value = "";
    return await listQualityTargets({
      pageSize: targetPageSize.value,
      pageToken,
      search: appliedTargetSearch.value,
      orderBy: targetOrderBy.value,
      signal,
    });
  },
  onError: (e) => {
    targetsError.value = handleError(e, "dataQuality.targets.loadFailed");
  },
});

// The template reads the rows through plain helpers; the paged ref is the one
// source, so the expander and the scroll watcher see the same rows.

function applyTargetSearch() {
  appliedTargetSearch.value = targetSearch.value.trim();
  void resetTargets();
}

/** One request answers a pause in typing, not every keystroke. */
const debouncedTargetSearch = useDebounceFn(() => applyTargetSearch(), 300);

watch(targetSearch, () => void debouncedTargetSearch());

watch(targetSort, () => void resetTargets());
watch(targetPageSize, () => void resetTargets());

function clearTargetSearch() {
  targetSearch.value = "";
  applyTargetSearch();
}

// ---- target rows: expansion from the URL ----

const expandedTargetGuid = computed(() =>
  flatView.value ? "" : queryString(route.query.target)
);
const expandedRun = computed(() =>
  flatView.value ? "" : queryString(route.query.run)
);

function toggleTarget(summary: QualityTargetSummary) {
  const guid = targetGuid(summary);
  if (!guid) {
    return;
  }
  if (expandedTargetGuid.value === guid) {
    setQuery({ target: undefined, run: undefined });
    return;
  }
  setQuery({ target: guid, run: undefined });
}

function onRunToggle(name: string) {
  setQuery({ run: name || undefined });
}

/**
 * A deep link names the target to expand; once its row exists, bring it into
 * view. The scroll is best effort (the workspace shell owns the scroll
 * container, and a test environment has no scrolling).
 */
watch(
  [targets, expandedTargetGuid, targetsLoading],
  async () => {
    const guid = expandedTargetGuid.value;
    if (!guid || targetsLoading.value) {
      return;
    }
    if (!targets.value.some((summary) => targetGuid(summary) === guid)) {
      return;
    }
    await nextTick();
    document
      .querySelector<HTMLElement>(`[data-target-guid="${guid}"]`)
      ?.scrollIntoView?.({ block: "center" });
  },
  { flush: "post" }
);

// ---- target row helpers ----

function targetGuid(summary: QualityTargetSummary): string {
  return summary.target?.metaGuid ?? "";
}

function targetName(summary: QualityTargetSummary): string {
  return summary.target?.path?.object || summary.target?.metaGuid || "-";
}

/** The target's coarse scope and path, e.g. `prod · public.orders`. */
function targetPathLine(summary: QualityTargetSummary): string {
  const path = summary.target?.path;
  const scope = targetScopeLabel(path);
  const name = formatTargetPath(path);
  return scope === "-" ? name : `${scope} · ${name}`;
}

function targetRoute(summary: QualityTargetSummary): RouteLocationRaw {
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(targetGuid(summary)) },
    query: {
      metaType: String(summary.target?.metaType ?? MetaType.UNSPECIFIED),
    },
  };
}

function latestRun(summary: QualityTargetSummary) {
  return summary.latestRun;
}

function runCounts(
  summary: QualityTargetSummary
): QualityStatusCounts | undefined {
  return summary.latestRun?.counts;
}

function runState(summary: QualityTargetSummary): QualityExecutionState {
  return summary.latestRun?.state ?? QualityExecutionState.UNSPECIFIED;
}

function runOutcome(summary: QualityTargetSummary): QualityStatus {
  return summary.latestRun?.outcome ?? QualityStatus.UNSPECIFIED;
}

const TARGET_VERDICT_ORDER: QualityStatus[] = [
  QualityStatus.PASS,
  QualityStatus.WARN,
  QualityStatus.FAIL,
  QualityStatus.ERROR,
  QualityStatus.SKIPPED,
  QualityStatus.NOT_EVALUATED,
];

function verdictCount(
  counts: QualityStatusCounts | undefined,
  status: QualityStatus
): number {
  if (!counts) {
    return 0;
  }
  switch (status) {
    case QualityStatus.PASS:
      return counts.pass;
    case QualityStatus.WARN:
      return counts.warn;
    case QualityStatus.FAIL:
      return counts.fail;
    case QualityStatus.ERROR:
      return counts.error;
    case QualityStatus.SKIPPED:
      return counts.skipped;
    case QualityStatus.NOT_EVALUATED:
      return counts.notEvaluated;
    default:
      return 0;
  }
}

/** The verdicts the latest batch recorded, e.g. `3 Pass · 1 Fail`. */
function countsSummary(summary: QualityTargetSummary): string {
  const counts = runCounts(summary);
  if (!counts) {
    return "";
  }
  return TARGET_VERDICT_ORDER.map((status) => ({
    status,
    count: verdictCount(counts, status),
  }))
    .filter((verdict) => verdict.count > 0)
    .map((verdict) => `${verdict.count} ${t(qualityStatusKey(verdict.status))}`)
    .join(" · ");
}

/** Checks that failed or errored in the latest batch. */
function failedChecks(summary: QualityTargetSummary): number {
  const counts = runCounts(summary);
  if (!counts) {
    return 0;
  }
  return counts.fail + counts.error;
}

/** Planned checks that never reached a verdict in the latest batch. */
function unfinishedChecks(summary: QualityTargetSummary): number {
  const counts = runCounts(summary);
  if (!counts) {
    return 0;
  }
  const finished = TARGET_VERDICT_ORDER.reduce(
    (sum, status) => sum + verdictCount(counts, status),
    0
  );
  return Math.max(0, counts.total - finished);
}

/**
 * The target's enabled rule count when a scheduled batch covered only a slice of
 * an oversize target (batches are target-complete, but a legacy oversize target
 * may be split). Only a batch at the composition cap is disclosed, so a retry
 * batch or a check added since the run never reads as partial.
 */
function partialChecks(summary: QualityTargetSummary): number {
  return partialQualityRunChecks(summary.latestRun, summary.enabledRuleCount);
}

function scanTime(summary: QualityTargetSummary) {
  const run = latestRun(summary);
  return run?.finishTime ?? run?.startTime;
}

function scanTimeLabel(summary: QualityTargetSummary): string {
  return formatRelativeTime(scanTime(summary), locale.value);
}

function scanTimeTitle(summary: QualityTargetSummary): string {
  return formatDateTime(scanTime(summary), locale.value);
}

function runDurationLabel(summary: QualityTargetSummary): string {
  const run = latestRun(summary);
  if (!run) {
    return "-";
  }
  const seconds = executionDurationSeconds({
    startTime: run.startTime,
    finishTime: run.finishTime,
  });
  return seconds === undefined ? "-" : formatSeconds(seconds);
}

/** A batch can be queued, running or abandoned; only the last two need words. */
function runStateHint(summary: QualityTargetSummary): string {
  switch (runState(summary)) {
    case QualityExecutionState.PENDING:
    case QualityExecutionState.RUNNING:
      return t("dataQuality.executions.runRunningHint");
    case QualityExecutionState.ABANDONED:
      return t("dataQuality.executions.runAbandonedHint");
    default:
      return "";
  }
}

function isExpanded(summary: QualityTargetSummary): boolean {
  return (
    expandedTargetGuid.value !== "" &&
    expandedTargetGuid.value === targetGuid(summary)
  );
}

// ---- by execution (flat) ----

const state = ref("-");
const outcome = ref("-");
const trigger = ref("-");
const appliedState = ref("-");
const appliedOutcome = ref("-");
const appliedTrigger = ref("-");

const executionsPageSize = ref(50);
const executionsError = ref("");

const stateOptions: Array<{ value: QualityExecutionState; key: string }> = [
  {
    value: QualityExecutionState.PENDING,
    key: "dataQuality.executionState.pending",
  },
  {
    value: QualityExecutionState.RUNNING,
    key: "dataQuality.executionState.running",
  },
  {
    value: QualityExecutionState.COMPLETED,
    key: "dataQuality.executionState.completed",
  },
  {
    value: QualityExecutionState.ABANDONED,
    key: "dataQuality.executionState.abandoned",
  },
];

const outcomeOptions: Array<{ value: QualityStatus; key: string }> = [
  { value: QualityStatus.PASS, key: "dataQuality.status.pass" },
  { value: QualityStatus.WARN, key: "dataQuality.status.warn" },
  { value: QualityStatus.FAIL, key: "dataQuality.status.fail" },
  { value: QualityStatus.ERROR, key: "dataQuality.status.error" },
  { value: QualityStatus.SKIPPED, key: "dataQuality.status.skipped" },
  {
    value: QualityStatus.NOT_EVALUATED,
    key: "dataQuality.status.notEvaluated",
  },
];

const triggerOptions: Array<{ value: QualityExecutionTrigger; key: string }> = [
  {
    value: QualityExecutionTrigger.MANUAL,
    key: "dataQuality.executionTrigger.manual",
  },
  {
    value: QualityExecutionTrigger.SCHEDULED,
    key: "dataQuality.executionTrigger.scheduled",
  },
];

/** The rule resource name carried by `?rule=`, if any. */
const ruleFilter = computed(() => queryString(route.query.rule));
/** The target GUID carried by `?target=`, if any (flat view: a filter). */
const targetFilter = computed(() => queryString(route.query.target));
/** The run resource name carried by `?run=`, if any (flat view: a filter). */
const runFilter = computed(() => queryString(route.query.run));
/** A run id reads better than the full resource name in a chip. */
const runShortID = computed(
  () => runFilter.value.split("/").pop() || runFilter.value
);

const hasActiveFilters = computed(
  () =>
    appliedState.value !== "-" ||
    appliedOutcome.value !== "-" ||
    appliedTrigger.value !== "-" ||
    ruleFilter.value !== "" ||
    targetFilter.value !== "" ||
    runFilter.value !== ""
);

const {
  items: executions,
  isLoading: executionsLoading,
  hasNext: executionsHasNext,
  hasPrevious: executionsHasPrevious,
  reset: resetExecutions,
  refresh: refreshExecutions,
  goNext: goNextExecutions,
  goPrevious: goPreviousExecutions,
} = usePagedFetch<QualityExecution>({
  fetchPage: async (pageToken, signal) => {
    executionsError.value = "";
    return await listQualityExecutions({
      pageSize: executionsPageSize.value,
      pageToken,
      state: enumFromFilter(appliedState.value, [
        QualityExecutionState.UNSPECIFIED,
        QualityExecutionState.PENDING,
        QualityExecutionState.RUNNING,
        QualityExecutionState.COMPLETED,
        QualityExecutionState.ABANDONED,
      ]),
      outcome: enumFromFilter(appliedOutcome.value, [
        QualityStatus.UNSPECIFIED,
        QualityStatus.PASS,
        QualityStatus.WARN,
        QualityStatus.FAIL,
        QualityStatus.ERROR,
        QualityStatus.SKIPPED,
        QualityStatus.NOT_EVALUATED,
      ]),
      trigger: enumFromFilter(appliedTrigger.value, [
        QualityExecutionTrigger.UNSPECIFIED,
        QualityExecutionTrigger.MANUAL,
        QualityExecutionTrigger.SCHEDULED,
      ]),
      rule: ruleFilter.value,
      targetGuid: targetFilter.value,
      run: runFilter.value,
      signal,
    });
  },
  onError: (e) => {
    executionsError.value = handleError(e, "dataQuality.fetchExecutionsError");
  },
});

// A cursor belongs to the query that produced it, so a new page size restarts
// the walk at the first page.
watch(executionsPageSize, () => void resetExecutions());

// The URL is the filter for the cross-page links: a rule, target or run page
// that points here must reload when the query changes, not only on mount.
watch(
  () => [ruleFilter.value, targetFilter.value, runFilter.value],
  () => {
    // The URL filters are already read by `fetchPage`; keep any half-edited
    // Select values un-applied and just restart the cursor walk.
    if (flatView.value) {
      void resetExecutions();
    }
  }
);

/**
 * The Select's value as an enum member. `-` and any value the enum no longer
 * has mean "no filter", so a stale URL or a hand-edited state cannot break the
 * request.
 */
function enumFromFilter<T extends number>(value: string, members: T[]): T {
  const parsed = Number(value);
  return members.includes(parsed as T) ? (parsed as T) : members[0];
}

function applyFilters() {
  appliedState.value = state.value;
  appliedOutcome.value = outcome.value;
  appliedTrigger.value = trigger.value;
  void resetExecutions();
}

function clearFilters() {
  state.value = "-";
  outcome.value = "-";
  trigger.value = "-";
  // The applied values, not only the Select model: clearing while a URL filter
  // is present must not leave the previous request's filters in force.
  appliedState.value = "-";
  appliedOutcome.value = "-";
  appliedTrigger.value = "-";
  const query = { ...route.query };
  delete query.rule;
  delete query.target;
  delete query.run;
  if (Object.keys(query).length !== Object.keys(route.query).length) {
    void router.replace({ query });
    return;
  }
  void resetExecutions();
}

function clearRuleFilter() {
  const query = { ...route.query };
  delete query.rule;
  void router.replace({ query });
}

function clearTargetFilter() {
  const query = { ...route.query };
  delete query.target;
  void router.replace({ query });
}

function clearRunFilter() {
  const query = { ...route.query };
  delete query.run;
  // A run-only deep link is the flat list by derivation; dropping the chip must
  // keep the reader in that list instead of bouncing to the target view.
  if (!viewParam.value) {
    query.view = "executions";
  }
  void router.replace({ query });
}

function ruleLabel(ruleName: string): string {
  return ruleTitles.value[ruleName] || qualityRuleID(ruleName);
}

function ruleRoute(ruleName: string) {
  return {
    name: "DataQualityRuleRedirect",
    params: { ruleId: ruleName.split("/") },
  };
}

/** Whether the execution carries the target its rule watched. */
function hasTarget(execution: QualityExecution): boolean {
  return Boolean(execution.target?.metaGuid);
}

/**
 * The target workspace for one execution, opening the check drawer when a check
 * name is given. Undefined when the execution carries no target: historical
 * rows can predate the target snapshot.
 */
function targetWorkspaceRoute(
  execution: QualityExecution,
  check = ""
): RouteLocationRaw | undefined {
  const target = execution.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  // A snapshot without a type is not an error: the target page derives it from
  // the summary or the rules, so the query carries only what it knows.
  const query: Record<string, string> = {};
  if (target.metaType !== MetaType.UNSPECIFIED) {
    query.metaType = String(target.metaType);
  }
  if (check) {
    query.check = check;
  }
  return {
    name: "DataQualityTarget",
    params: { targetGuid: guidToRouteParams(target.metaGuid) },
    query,
  };
}

/**
 * A rule name links into its target's workspace with the check selected. A row
 * without a target snapshot falls back to the retired per-rule URL, whose
 * redirect resolves the target itself.
 */
function ruleTargetRoute(execution: QualityExecution): RouteLocationRaw {
  return (
    targetWorkspaceRoute(execution, execution.rule) ?? ruleRoute(execution.rule)
  );
}

/** The target itself, without selecting a check. */
function executionTargetRoute(execution: QualityExecution): RouteLocationRaw {
  return targetWorkspaceRoute(execution) ?? ruleRoute(execution.rule);
}

function targetPath(execution: QualityExecution): string {
  const path = formatTargetPath(execution.target?.path);
  return path === "-" ? execution.target?.metaGuid || "-" : path;
}

/** Every target's instance and path, for the cell's tooltip. */
function targetTitle(execution: QualityExecution): string {
  const path = targetPath(execution);
  const scope = targetScopeLabel(execution.target?.path);
  return scope === "-" ? path : `${scope} · ${path}`;
}

/** The verdict's measured value, or "-" while there is no verdict yet. */
function observedLabel(execution: QualityExecution): string {
  if (execution.outcome === QualityStatus.UNSPECIFIED) {
    return "-";
  }
  return formatObservedValue(
    execution.observedValue,
    execution.observedKind,
    locale.value
  );
}

function rowsLabel(execution: QualityExecution): string {
  if (execution.outcome === QualityStatus.UNSPECIFIED) {
    return "-";
  }
  return `${formatRowCount(execution.failedRows, locale.value)} / ${formatRowCount(execution.totalRows, locale.value)}`;
}

function durationLabel(execution: QualityExecution): string {
  const seconds = executionDurationSeconds(execution);
  return seconds === undefined ? "-" : formatSeconds(seconds);
}

/** A click that ends a text selection is a copy, not a navigation. */
function openExecution(execution: QualityExecution) {
  if (window.getSelection()?.toString()) {
    return;
  }
  void router.push({
    name: "DataQualityExecutionDetail",
    params: { executionId: execution.name.split("/") },
  });
}

// ---- loading and polling ----

const activeLoading = computed(() =>
  flatView.value ? executionsLoading.value : targetsLoading.value
);

function refreshCurrent() {
  if (flatView.value) {
    void refreshExecutions();
  } else {
    void refreshTargets();
  }
}

function hasInProgress(): boolean {
  return executions.value.some((execution) =>
    isExecutionInProgress(execution.state)
  );
}

// The poll keeps the table honest while the runner works; a manual refresh is
// not needed to see PENDING become COMPLETED.
const executionsPolling = usePolling(() => refreshExecutions(), {
  intervalMs: POLL_INTERVAL_MS,
  shouldContinue: hasInProgress,
});

// A flat page that shows a pending/running execution reloads until none is
// left, so the state the user clicked through from is not frozen on screen.
watch(executions, () => {
  if (flatView.value && hasInProgress()) {
    executionsPolling.start();
  } else {
    executionsPolling.stop();
  }
});

// Switching views re-reads the view's first page rather than showing another
// view's rows under different columns.
watch(flatView, (flat) => {
  if (flat) {
    void resetExecutions();
  } else {
    executionsPolling.stop();
    void resetTargets();
  }
});

onMounted(() => {
  void loadRuleTitles();
  if (flatView.value) {
    void resetExecutions();
  } else {
    void resetTargets();
  }
});
</script>
