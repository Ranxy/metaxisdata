<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('dataQualitySettings.title')"
      :description="t('dataQualitySettings.description')"
    >
      <template #actions>
        <span
          v-if="!canUpdate"
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQualitySettings.readOnlyHint") }}
        </span>
        <span
          v-else-if="isDirty && !isLoading && !error"
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQualitySettings.unsavedHint") }}
        </span>
        <Button
          :disabled="isLoading || !!error || isSaving || !canUpdate"
          @click="handleSave"
        >
          {{ t("common.save") }}
        </Button>
      </template>
    </PageHeader>

    <PageState
      :loading="isLoading"
      :error="error"
    >
      <template #error>
        <Button
          variant="outline"
          size="sm"
          @click="load"
        >
          {{ t("common.refresh") }}
        </Button>
      </template>

      <div class="flex flex-col gap-4">
        <Alert
          v-if="saveError"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQualitySettings.saveFailed") }}</AlertTitle>
          <AlertDescription class="break-words">
            {{ saveError }}
          </AlertDescription>
        </Alert>

        <Alert
          v-if="issues.length > 0"
          variant="destructive"
        >
          <AlertTriangle class="h-4 w-4" />
          <AlertTitle>{{ t("dataQualitySettings.validationTitle") }}</AlertTitle>
          <AlertDescription>
            <ul class="list-disc space-y-1 pl-5">
              <li
                v-for="issue in issues"
                :key="issue.key"
              >
                {{ t(issue.key, issue.params ?? {}) }}
              </li>
            </ul>
          </AlertDescription>
        </Alert>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.llmSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.llmSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-enable-llm"
                :checked="form.enableLlmAnalysis"
                :disabled="!canUpdate"
                @update:checked="form.enableLlmAnalysis = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-enable-llm">
                  {{ t("dataQualitySettings.enableLlmAnalysis") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.enableLlmAnalysisHint") }}
                </p>
              </div>
            </div>

            <div class="space-y-1.5">
              <Label for="dq-setting-llm-profile">
                {{ t("dataQualitySettings.llmProfile") }}
              </Label>
              <Select
                :model-value="form.llmProviderProfile || AUTO_PROFILE"
                :disabled="!canUpdate || llmDisabled(form)"
                @update:model-value="setLlmProfile"
              >
                <SelectTrigger id="dq-setting-llm-profile">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem :value="AUTO_PROFILE">
                    {{ t("dataQualitySettings.llmProfileAuto") }}
                  </SelectItem>
                  <SelectItem
                    v-for="profile in profileOptions"
                    :key="profile.name"
                    :value="profile.name"
                  >
                    {{ profile.title }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p class="text-sm text-muted-foreground">
                {{ llmDisabled(form) ? t("dataQualitySettings.llmDisabledHint") : t("dataQualitySettings.llmProfileHint") }}
              </p>
            </div>

            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-sample-values"
                :checked="form.sendSampleValuesToLlm"
                :disabled="!canUpdate || llmDisabled(form)"
                @update:checked="form.sendSampleValuesToLlm = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-sample-values">
                  {{ t("dataQualitySettings.sendSampleValues") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.sendSampleValuesHint") }}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.samplesSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.samplesSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-store-samples"
                :checked="form.storeFailureSamples"
                :disabled="!canUpdate"
                @update:checked="form.storeFailureSamples = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-store-samples">
                  {{ t("dataQualitySettings.storeFailureSamples") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.storeFailureSamplesHint") }}
                </p>
              </div>
            </div>
            <FormField
              id="dq-setting-sample-limit"
              v-model="form.failureSampleLimit"
              type="number"
              class="max-w-xs"
              :disabled="!canUpdate"
              :label="t('dataQualitySettings.failureSampleLimit')"
              :hint="t('dataQualitySettings.failureSampleLimitHint')"
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.limitsSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.limitsSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <div class="space-y-1.5">
              <Label for="dq-setting-schedule">
                {{ t("dataQualitySettings.defaultSchedule") }}
              </Label>
              <div class="flex max-w-xs gap-2">
                <FormField
                  id="dq-setting-schedule"
                  v-model="form.defaultSchedule.value"
                  type="number"
                  class="min-w-0 flex-1"
                  :disabled="!canUpdate"
                />
                <Select
                  :model-value="form.defaultSchedule.unit"
                  :disabled="!canUpdate"
                  @update:model-value="setScheduleUnit"
                >
                  <SelectTrigger class="w-32">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="minutes">
                      {{ t("dataQuality.editor.unitMinutes") }}
                    </SelectItem>
                    <SelectItem value="hours">
                      {{ t("dataQuality.editor.unitHours") }}
                    </SelectItem>
                    <SelectItem value="days">
                      {{ t("dataQuality.editor.unitDays") }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQualitySettings.defaultScheduleHint") }}
              </p>
            </div>

            <div class="grid gap-4 sm:grid-cols-3">
              <FormField
                id="dq-setting-daily-executions"
                v-model="form.dailyExecutionLimit"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.dailyExecutionLimit')"
                :hint="t('dataQualitySettings.dailyExecutionLimitHint')"
              />
              <FormField
                id="dq-setting-daily-columns"
                v-model="form.dailyColumnAnalysisLimit"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.dailyColumnAnalysisLimit')"
                :hint="t('dataQualitySettings.dailyColumnAnalysisLimitHint')"
              />
              <FormField
                id="dq-setting-daily-llm-analyses"
                v-model="form.dailyLlmAnalysisLimit"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.dailyLlmAnalysisLimit')"
                :hint="t('dataQualitySettings.dailyLlmAnalysisLimitHint')"
              />
              <FormField
                id="dq-setting-llm-concurrency"
                v-model="form.llmConcurrencyLimit"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.llmConcurrencyLimit')"
                :hint="t('dataQualitySettings.llmConcurrencyLimitHint')"
              />
              <FormField
                id="dq-setting-max-columns"
                v-model="form.maxColumnsPerAnalysis"
                type="number"
                :disabled="!canUpdate"
                :label="t('dataQualitySettings.maxColumnsPerAnalysis')"
                :hint="t('dataQualitySettings.maxColumnsPerAnalysisHint')"
              />
            </div>

            <!-- The zone the time variables resolve in; it does not affect a
                 check's schedule, which belongs to the target. -->
            <FormField
              id="dq-setting-check-timezone"
              v-model="form.checkTimeZone"
              class="max-w-xs"
              :disabled="!canUpdate"
              :label="t('dataQualitySettings.checkTimeZone')"
              :hint="t('dataQualitySettings.checkTimeZoneHint')"
              placeholder="Asia/Shanghai"
            />
          </CardContent>
        </Card>

        <!-- Code tables are workspace data, not settings, but they are authored
             next to the checks that use them and share this page's write model. -->
        <Card v-if="canListCodeTables">
          <CardHeader>
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="space-y-1.5">
                <CardTitle>{{ t("dataQualitySettings.codeTablesSection") }}</CardTitle>
                <CardDescription>
                  {{ t("dataQualitySettings.codeTablesSectionDescription") }}
                </CardDescription>
              </div>
              <Button
                v-if="canCreateCodeTable"
                size="sm"
                class="shrink-0"
                data-add-code-table
                @click="openCreateCodeTable"
              >
                <Plus class="mr-2 h-4 w-4" />
                {{ t("dataQualitySettings.addCodeTable") }}
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <Alert
              v-if="codeTableActionError"
              variant="destructive"
              class="mb-4"
            >
              <AlertTriangle class="h-4 w-4" />
              <AlertTitle>{{ t("dataQualitySettings.codeTableDeleteFailed") }}</AlertTitle>
              <AlertDescription class="break-words">
                {{ codeTableActionError }}
              </AlertDescription>
            </Alert>
            <PageState :loading="codeTablesLoading">
              <div
                v-if="codeTablesError"
                class="space-y-2 text-sm text-muted-foreground"
              >
                <p>{{ t("dataQualitySettings.codeTableLoadFailed") }}</p>
                <Button
                  variant="outline"
                  size="sm"
                  @click="loadCodeTables"
                >
                  {{ t("common.refresh") }}
                </Button>
              </div>
              <EmptyState
                v-else-if="codeTables.length === 0"
                :icon="ListChecks"
                :title="t('dataQualitySettings.codeTableEmpty')"
                :description="t('dataQualitySettings.codeTableEmptyHint')"
              />
              <Table
                v-else
                class="table-fixed"
              >
                <TableHeader>
                  <TableRow>
                    <TableHead class="w-[28%]">
                      {{ t("dataQualitySettings.codeTableColumnTitle") }}
                    </TableHead>
                    <TableHead class="w-[32%]">
                      {{ t("dataQualitySettings.codeTableColumnDescription") }}
                    </TableHead>
                    <TableHead class="w-[14%]">
                      {{ t("dataQualitySettings.codeTableColumnValues") }}
                    </TableHead>
                    <TableHead class="w-[16%]">
                      {{ t("dataQualitySettings.codeTableColumnUpdatedAt") }}
                    </TableHead>
                    <TableHead class="w-[10%] text-right">
                      {{ t("common.actions") }}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  <TableRow
                    v-for="table in codeTables"
                    :key="table.name"
                  >
                    <TableCell>
                      <div
                        class="truncate font-medium"
                        :title="table.title"
                      >
                        {{ table.title || "-" }}
                      </div>
                    </TableCell>
                    <TableCell class="text-muted-foreground">
                      <div
                        class="truncate"
                        :title="table.description"
                      >
                        {{ table.description || "-" }}
                      </div>
                    </TableCell>
                    <TableCell>
                      {{ table.valueCount }}
                    </TableCell>
                    <TableCell class="text-muted-foreground">
                      <div
                        class="truncate"
                        :title="formatDateTime(table.updateTime, locale)"
                      >
                        {{ formatDateTime(table.updateTime, locale) }}
                      </div>
                    </TableCell>
                    <TableCell class="text-right">
                      <div class="flex items-center justify-end gap-1">
                        <Button
                          v-if="canUpdateCodeTable"
                          variant="ghost"
                          size="icon"
                          :title="t('common.edit')"
                          @click="openEditCodeTable(table)"
                        >
                          <Pencil class="h-4 w-4 text-muted-foreground" />
                        </Button>
                        <Button
                          v-if="canDeleteCodeTable"
                          variant="ghost"
                          size="icon"
                          :title="t('common.delete')"
                          @click="confirmDeleteCodeTable(table)"
                        >
                          <Trash2 class="h-4 w-4 text-muted-foreground hover:text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </PageState>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.agentSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.agentSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-3">
            <!-- The kill switch is the workspace's total gate for agent writes. Off
                 is the default: agents may read, and no agent write applies. -->
            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-agent-autonomy"
                :checked="form.agentAutonomyEnabled"
                :disabled="!canUpdate"
                @update:checked="form.agentAutonomyEnabled = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-agent-autonomy">
                  {{ t("dataQualitySettings.agentAutonomy") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.agentAutonomyHint") }}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.runnerSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.runnerSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-agent-runner"
                :checked="form.agentRunnerEnabled"
                :disabled="!canUpdate"
                @update:checked="form.agentRunnerEnabled = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-agent-runner">
                  {{ t("dataQualitySettings.runnerEnabled") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.runnerEnabledHint") }}
                </p>
              </div>
            </div>

            <div class="space-y-2">
              <Label>{{ t("dataQualitySettings.runnerDatabases") }}</Label>
              <p class="text-sm text-muted-foreground">
                {{ t("dataQualitySettings.runnerDatabasesHint") }}
              </p>
              <p
                v-if="runnerDatabases.length === 0"
                class="text-sm text-muted-foreground"
              >
                {{ t("dataQualitySettings.runnerDatabasesEmpty") }}
              </p>
              <div
                v-for="group in runnerDatabases"
                :key="group.instance"
                class="space-y-1"
              >
                <p class="text-sm font-medium">{{ group.title }}</p>
                <div class="grid gap-1 sm:grid-cols-2">
                  <label
                    v-for="database in group.databases"
                    :key="database.name"
                    class="flex items-center gap-2 text-sm"
                  >
                    <input
                      type="checkbox"
                      :data-testid="`dq-runner-db-${database.name}`"
                      :checked="form.agentRunnerDatabases.includes(database.name)"
                      :disabled="!canUpdate"
                      @change="toggleRunnerDatabase(database.name)"
                    />
                    {{ database.name.split('/').pop() }}
                  </label>
                </div>
              </div>
            </div>

            <div class="grid gap-3 sm:grid-cols-3">
              <div class="space-y-1.5">
                <Label for="dq-setting-runner-tokens">
                  {{ t("dataQualitySettings.runnerTokensPerHour") }}
                </Label>
                <Input
                  id="dq-setting-runner-tokens"
                  v-model="form.agentRunnerTokensPerHour"
                  inputmode="numeric"
                  :disabled="!canUpdate"
                />
                <p class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.runnerTokensPerHourHint") }}
                </p>
              </div>
              <div class="space-y-1.5">
                <Label for="dq-setting-runner-goals">
                  {{ t("dataQualitySettings.runnerGoalsPerHour") }}
                </Label>
                <Input
                  id="dq-setting-runner-goals"
                  v-model="form.agentRunnerGoalsPerHour"
                  inputmode="numeric"
                  :disabled="!canUpdate"
                />
              </div>
              <div class="space-y-1.5">
                <Label for="dq-setting-runner-concurrency">
                  {{ t("dataQualitySettings.runnerConcurrency") }}
                </Label>
                <Input
                  id="dq-setting-runner-concurrency"
                  v-model="form.agentRunnerMaxConcurrentGoals"
                  inputmode="numeric"
                  :disabled="!canUpdate"
                />
              </div>
            </div>
          </CardContent>
        </Card>

        <QualityAgentsCard
          v-if="canUpdate"
          :can-update="canUpdate"
        />

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.digestSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.digestSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent class="space-y-5">
            <div class="flex items-start gap-3">
              <Checkbox
                id="dq-setting-digest-enabled"
                :checked="form.weeklyDigest.enabled"
                :disabled="!canUpdate"
                @update:checked="form.weeklyDigest.enabled = $event === true"
              />
              <div class="grid gap-1">
                <Label for="dq-setting-digest-enabled">
                  {{ t("dataQualitySettings.digestEnabled") }}
                </Label>
                <p class="text-sm text-muted-foreground">
                  {{ t("dataQualitySettings.digestEnabledHint") }}
                </p>
              </div>
            </div>

            <div class="grid gap-4 sm:grid-cols-3">
              <div class="space-y-2.5">
                <Label for="dq-setting-digest-day">
                  {{ t("dataQualitySettings.digestDay") }}
                </Label>
                <Select
                  :model-value="String(form.weeklyDigest.dayOfWeek)"
                  :disabled="digestDisabled"
                  @update:model-value="setDigestDay"
                >
                  <SelectTrigger
                    id="dq-setting-digest-day"
                    class="w-full"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem
                      v-for="option in digestDayOptions"
                      :key="option.value"
                      :value="String(option.value)"
                    >
                      {{ option.label }}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <FormField
                id="dq-setting-digest-time"
                v-model="form.weeklyDigest.time"
                type="time"
                :disabled="digestDisabled"
                :label="t('dataQualitySettings.digestTime')"
                :hint="t('dataQualitySettings.digestTimeHint')"
              />
              <FormField
                id="dq-setting-digest-timezone"
                v-model="form.weeklyDigest.timezone"
                :disabled="digestDisabled"
                :label="t('dataQualitySettings.digestTimezone')"
                :hint="t('dataQualitySettings.digestTimezoneHint')"
                placeholder="Asia/Shanghai"
              />
            </div>

            <p class="text-sm text-muted-foreground">
              {{ t("dataQualitySettings.digestDeliveryHint") }}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{{ t("dataQualitySettings.usageSection") }}</CardTitle>
            <CardDescription>
              {{ t("dataQualitySettings.usageSectionDescription") }}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p
              v-if="!canSuggest"
              class="text-sm text-muted-foreground"
            >
              {{ t("dataQualitySettings.usageNoPermission") }}
            </p>
            <div
              v-else-if="usageLoading"
              class="flex items-center gap-2 text-sm text-muted-foreground"
            >
              <Loader2 class="h-4 w-4 animate-spin" />
              {{ t("common.loading") }}
            </div>
            <Alert
              v-else-if="usageError"
              variant="warning"
            >
              <AlertTriangle class="h-4 w-4" />
              <AlertTitle>{{ t("dataQualitySettings.usageFetchFailed") }}</AlertTitle>
              <AlertDescription class="space-y-2">
                <p class="break-words">{{ usageError }}</p>
                <Button
                  variant="outline"
                  size="sm"
                  @click="loadUsage"
                >
                  {{ t("common.refresh") }}
                </Button>
              </AlertDescription>
            </Alert>
            <div
              v-else-if="usage"
              class="grid gap-4 sm:grid-cols-2"
            >
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.usageDailyAnalyses") }}
                </div>
                <div class="text-sm font-medium">
                  {{ ratio(usage.dailyAnalysisUsed, usage.dailyAnalysisLimit) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.usageDailyColumnAnalyses") }}
                </div>
                <div class="text-sm font-medium">
                  {{ ratio(usage.dailyColumnAnalysisUsed, usage.dailyColumnAnalysisLimit) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.usageConcurrency") }}
                </div>
                <div class="text-sm font-medium">
                  {{ ratio(usage.inFlight, usage.concurrencyLimit) }}
                </div>
              </div>
              <div>
                <div class="text-xs text-muted-foreground">
                  {{ t("dataQualitySettings.usageResetsAt") }}
                </div>
                <div
                  class="text-sm font-medium"
                  :title="formatDateTime(usage.resetsAt, locale)"
                >
                  {{ formatRelativeTime(usage.resetsAt, locale) }}
                </div>
              </div>
            </div>
            <p
              v-else
              class="text-sm text-muted-foreground"
            >
              -
            </p>
          </CardContent>
        </Card>
      </div>
    </PageState>

    <!-- Create or edit one code table. The values are edited as text or imported
         from a CSV client-side; both paths trim, skip blanks/comments and
         de-duplicate before anything is sent. -->
    <Dialog v-model:open="codeTableDialogOpen">
      <DialogContent class="max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {{
              editingCodeTable
                ? t("dataQualitySettings.editCodeTable")
                : t("dataQualitySettings.addCodeTable")
            }}
          </DialogTitle>
          <DialogDescription>
            {{ t("dataQualitySettings.codeTablesSectionDescription") }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4">
          <FormField
            id="dq-code-table-title"
            v-model="codeTableForm.title"
            required
            :error="codeTableTitleError"
            :label="t('dataQualitySettings.codeTableTitle')"
            :placeholder="t('dataQualitySettings.codeTableTitlePlaceholder')"
          />
          <div class="space-y-2">
            <Label for="dq-code-table-description">
              {{ t("dataQualitySettings.codeTableDescription") }}
            </Label>
            <textarea
              id="dq-code-table-description"
              v-model="codeTableForm.description"
              class="min-h-16 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              :placeholder="t('dataQualitySettings.codeTableDescriptionPlaceholder')"
            />
          </div>
          <div class="space-y-2">
            <Label for="dq-code-table-values">
              {{ t("dataQualitySettings.codeTableValues") }}
            </Label>
            <textarea
              id="dq-code-table-values"
              v-model="codeTableForm.values"
              class="min-h-40 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
              :placeholder="t('dataQualitySettings.codeTableValuesPlaceholder')"
              :disabled="codeTableLoading"
            />
            <p class="text-sm text-muted-foreground">
              {{ t("dataQualitySettings.codeTableValuesHint") }}
            </p>
          </div>
          <div class="space-y-2">
            <Label for="dq-code-table-csv">
              {{ t("dataQualitySettings.codeTableImportCsv") }}
            </Label>
            <Input
              id="dq-code-table-csv"
              type="file"
              accept=".csv,text/csv"
              :disabled="codeTableLoading"
              @change="onCodeTableCsv"
            />
            <p class="text-sm text-muted-foreground">
              {{ t("dataQualitySettings.codeTableImportHint") }}
            </p>
          </div>
          <p
            v-if="codeTableDialogError"
            class="text-sm text-destructive"
          >
            {{ codeTableDialogError }}
          </p>
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            :disabled="codeTableSaving"
            @click="codeTableDialogOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="codeTableSaving || codeTableLoading"
            data-save-code-table
            @click="saveCodeTable"
          >
            {{ codeTableSaving ? t("common.loading") : t("common.save") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- A referenced table is refused server-side; the card shows that message
         instead of pretending the delete succeeded. -->
    <ConfirmDeleteDialog
      v-model="codeTableDeleteOpen"
      :title="t('dataQualitySettings.deleteCodeTable')"
      :message="t('dataQualitySettings.deleteCodeTableConfirm')"
      :item-name="codeTableToDelete?.title ?? ''"
      :loading="codeTableDeleting"
      @confirm="deleteCodeTable"
    />
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import {
  AlertTriangle,
  ListChecks,
  Loader2,
  Pencil,
  Plus,
  Trash2,
} from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  createQualityCodeTable,
  deleteQualityCodeTable,
  getQualityCodeTable,
  getQualityLLMUsage,
  listAllQualityCodeTables,
  updateQualityCodeTable,
} from "@/api/data-quality";
import { listDatabases } from "@/api/database";
import { listAllInstances } from "@/api/instance";
import { listProfiles } from "@/api/llm";
import {
  getWorkspaceProfileSetting,
  updateDataQualitySetting,
} from "@/api/setting";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import QualityAgentsCard from "@/components/data-quality/QualityAgentsCard.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import FormField from "@/components/ui/form-field/FormField.vue";
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
import { useErrorMessage } from "@/composables/useErrorHandler";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityCodeTable,
  QualityCodeTableSchema,
  type QualityLLMUsage,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { DayOfWeek } from "@/types/proto-es/v1/setting_service_pb";
import { formatRowCount } from "@/utils/dataQuality";
import type { ScheduleUnit } from "@/utils/dataQualityForm";
import {
  type DataQualitySettingsForm,
  DIGEST_WEEKDAYS,
  formToSetting,
  llmDisabled,
  settingsToForm,
  validateSettingsForm,
} from "@/utils/dataQualitySettings";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import { isProviderAllowed } from "@/utils/llmProvider";
import {
  joinCodeTableValues,
  mergeCodeTableValues,
  parseCodeTableCsv,
  parseCodeTableLines,
} from "@/utils/qualityCodeTable";

/**
 * The data quality subsystem's workspace configuration. Every field is part of
 * `WorkspaceProfileSetting.data_quality`; the server returns the effective
 * values (its defaults filled in), and a save writes only that field.
 */

const { t, locale } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const form = ref<DataQualitySettingsForm>(settingsToForm(undefined));
interface RunnerDatabaseGroup {
  instance: string;
  title: string;
  databases: Array<{ name: string }>;
}
const runnerDatabases = ref<RunnerDatabaseGroup[]>([]);

async function loadRunnerDatabases() {
  if (!canUpdate.value) {
    return;
  }
  try {
    const instances = await listAllInstances();
    const groups: RunnerDatabaseGroup[] = [];
    for (const instance of instances) {
      const response = await listDatabases({
        parent: instance.name,
        pageSize: 100,
      });
      groups.push({
        instance: instance.name,
        title:
          instance.title || instance.name.split("/").pop() || instance.name,
        databases: response.databases.map((database) => ({
          name: database.name,
        })),
      });
    }
    runnerDatabases.value = groups;
  } catch {
    // A failed estate read leaves the saved allowlist untouched; the server still
    // enforces the scope.
    runnerDatabases.value = [];
  }
}

/** Toggle one database in the runner allowlist. */
function toggleRunnerDatabase(name: string) {
  const selected = form.value.agentRunnerDatabases;
  const index = selected.indexOf(name);
  if (index >= 0) {
    selected.splice(index, 1);
  } else {
    selected.push(name);
  }
}
const profileOptions = ref<Array<{ name: string; title: string }>>([]);
const issues = ref<ReturnType<typeof validateSettingsForm>>([]);
const isLoading = ref(true);
const isSaving = ref(false);
const error = ref("");
const saveError = ref("");
const original = ref("");
const usage = ref<QualityLLMUsage>();
const usageLoading = ref(false);
const usageError = ref("");

/** The code-table card: the list, its dialogs, and their own failures. */
const codeTables = ref<QualityCodeTable[]>([]);
const codeTablesLoading = ref(false);
const codeTablesError = ref(false);
const codeTableActionError = ref("");
const codeTableDialogOpen = ref(false);
const editingCodeTable = ref<QualityCodeTable>();
const codeTableForm = ref({ title: "", description: "", values: "" });
const codeTableTitleError = ref("");
const codeTableDialogError = ref("");
const codeTableSaving = ref(false);
/** Whether one table's values are still being read into the edit dialog. */
const codeTableLoading = ref(false);
const codeTableDeleteOpen = ref(false);
const codeTableToDelete = ref<QualityCodeTable>();
const codeTableDeleting = ref(false);

const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
);
// The usage RPC is part of the data-quality suggest surface, not settings.
const canSuggest = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.suggest")
);
// Code tables are a data-quality resource, not a settings field: each action has
// its own permission, and the card only exists for a reader who may list them.
const canListCodeTables = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.codeTables.list")
);
const canCreateCodeTable = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.codeTables.create")
);
const canUpdateCodeTable = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.codeTables.update")
);
const canDeleteCodeTable = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.codeTables.delete")
);

const isDirty = computed(() => JSON.stringify(form.value) !== original.value);

/**
 * Radix's Select cannot hold an empty string item, so "the first allowed
 * profile" needs a sentinel; it is mapped back to "" on the way into the form.
 */
const AUTO_PROFILE = "-";

function setLlmProfile(value: unknown) {
  form.value.llmProviderProfile =
    String(value) === AUTO_PROFILE ? "" : String(value);
}

function setScheduleUnit(value: unknown) {
  form.value.defaultSchedule.unit = String(value) as ScheduleUnit;
}

/** The digest's own fields mean nothing while the digest is off. */
const digestDisabled = computed(
  () => !canUpdate.value || !form.value.weeklyDigest.enabled
);

const digestDayOptions = computed(() => [
  { value: DayOfWeek.MONDAY, label: t("dataQualitySettings.weekdayMonday") },
  { value: DayOfWeek.TUESDAY, label: t("dataQualitySettings.weekdayTuesday") },
  {
    value: DayOfWeek.WEDNESDAY,
    label: t("dataQualitySettings.weekdayWednesday"),
  },
  {
    value: DayOfWeek.THURSDAY,
    label: t("dataQualitySettings.weekdayThursday"),
  },
  { value: DayOfWeek.FRIDAY, label: t("dataQualitySettings.weekdayFriday") },
  {
    value: DayOfWeek.SATURDAY,
    label: t("dataQualitySettings.weekdaySaturday"),
  },
  { value: DayOfWeek.SUNDAY, label: t("dataQualitySettings.weekdaySunday") },
]);

function setDigestDay(value: unknown) {
  const day = Number(value);
  if (DIGEST_WEEKDAYS.includes(day as DayOfWeek)) {
    form.value.weeklyDigest.dayOfWeek = day as DayOfWeek;
  }
}

async function load() {
  isLoading.value = true;
  error.value = "";
  saveError.value = "";
  codeTableActionError.value = "";
  try {
    const profile = await getWorkspaceProfileSetting();
    form.value = settingsToForm(profile.dataQuality);
    original.value = JSON.stringify(form.value);
    issues.value = [];
    await loadProfiles(profile.allowedLlmProviderProfiles ?? []);
    void loadUsage();
    void loadCodeTables();
  } catch (e: unknown) {
    error.value = formatError(e, "dataQualitySettings.fetchError");
  } finally {
    isLoading.value = false;
  }
}

/** The profiles the workspace allows and that have an enabled model. */
async function loadProfiles(allowed: string[]) {
  try {
    const response = await listProfiles({ pageSize: 100 });
    profileOptions.value = response.profiles
      .filter((profile) => profile.models.some((model) => model.enabled))
      .filter((profile) => isProviderAllowed(profile.name, allowed))
      .map((profile) => ({
        name: profile.name,
        title: profile.title || profile.name,
      }));
    // A configured profile the workspace no longer allows would render as an
    // empty select; clear it so the effective choice stays visible and honest.
    if (
      form.value.llmProviderProfile &&
      !profileOptions.value.some(
        (profile) => profile.name === form.value.llmProviderProfile
      )
    ) {
      form.value.llmProviderProfile = "";
    }
  } catch {
    profileOptions.value = [];
  }
}

/**
 * Read-only counters for the process-wide analysis quota. They are process
 * state, so they are refetched on load and again after a save applies new
 * limits (the save path calls `load`, which calls this).
 */
async function loadUsage() {
  if (!canSuggest.value) {
    usage.value = undefined;
    usageError.value = "";
    usageLoading.value = false;
    return;
  }
  usageLoading.value = true;
  usageError.value = "";
  try {
    usage.value = await getQualityLLMUsage();
  } catch (e: unknown) {
    usage.value = undefined;
    usageError.value = formatError(e, "dataQualitySettings.usageFetchFailed");
  } finally {
    usageLoading.value = false;
  }
}

/** `used / limit`, both localised. */
function ratio(used: number, limit: number): string {
  return t("dataQualitySettings.usageRatio", {
    used: formatRowCount(used, locale.value),
    limit: formatRowCount(limit, locale.value),
  });
}

/**
 * Read the code-table summaries. A failed read stays inside the card so the
 * settings form, which loaded fine, remains editable.
 */
async function loadCodeTables() {
  if (!canListCodeTables.value) {
    codeTables.value = [];
    return;
  }
  codeTablesLoading.value = true;
  codeTablesError.value = false;
  try {
    codeTables.value = await listAllQualityCodeTables();
  } catch {
    codeTables.value = [];
    codeTablesError.value = true;
  } finally {
    codeTablesLoading.value = false;
  }
}

function openCreateCodeTable() {
  editingCodeTable.value = undefined;
  codeTableForm.value = { title: "", description: "", values: "" };
  codeTableTitleError.value = "";
  codeTableDialogError.value = "";
  codeTableDialogOpen.value = true;
}

/**
 * Open one table for editing. The list carries summaries only, so the values
 * arrive from GetQualityCodeTable; a failed read leaves the dialog explaining
 * why it cannot save rather than sending an empty value list over the old one.
 */
async function openEditCodeTable(table: QualityCodeTable) {
  editingCodeTable.value = table;
  codeTableForm.value = {
    title: table.title,
    description: table.description,
    values: "",
  };
  codeTableTitleError.value = "";
  codeTableDialogError.value = "";
  codeTableDialogOpen.value = true;
  codeTableLoading.value = true;
  try {
    const full = await getQualityCodeTable(table.name);
    if (
      !codeTableDialogOpen.value ||
      editingCodeTable.value?.name !== full.name
    ) {
      return;
    }
    codeTableForm.value = {
      title: full.title,
      description: full.description,
      values: joinCodeTableValues(full.values),
    };
  } catch (e: unknown) {
    if (
      codeTableDialogOpen.value &&
      editingCodeTable.value?.name === table.name
    ) {
      codeTableDialogError.value = formatError(
        e,
        "dataQualitySettings.codeTableLoadFailed"
      );
    }
  } finally {
    codeTableLoading.value = false;
  }
}

/**
 * Append the codes a CSV contributed to the textarea. The parse is client-side
 * and never uploads the file: only the resulting list is sent with the table.
 */
async function onCodeTableCsv(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) {
    return;
  }
  try {
    const text = await file.text();
    codeTableForm.value.values = mergeCodeTableValues(
      codeTableForm.value.values,
      parseCodeTableCsv(text)
    );
    codeTableDialogError.value = "";
  } catch {
    codeTableDialogError.value = t("dataQualitySettings.codeTableCsvFailed");
  } finally {
    input.value = "";
  }
}

async function saveCodeTable() {
  const title = codeTableForm.value.title.trim();
  codeTableTitleError.value = "";
  codeTableDialogError.value = "";
  if (!title) {
    codeTableTitleError.value = t("dataQualitySettings.codeTableTitleRequired");
    return;
  }
  const values = parseCodeTableLines(codeTableForm.value.values);
  if (values.length === 0) {
    codeTableDialogError.value = t(
      "dataQualitySettings.codeTableValuesRequired"
    );
    return;
  }
  codeTableSaving.value = true;
  try {
    const editing = editingCodeTable.value;
    if (editing) {
      const updated = create(QualityCodeTableSchema, editing);
      updated.title = title;
      updated.description = codeTableForm.value.description.trim();
      updated.values = values;
      await updateQualityCodeTable(updated, ["title", "description", "values"]);
      notify.success(t("dataQualitySettings.codeTableUpdated"));
    } else {
      await createQualityCodeTable(
        create(QualityCodeTableSchema, {
          title,
          description: codeTableForm.value.description.trim(),
          values,
        })
      );
      notify.success(t("dataQualitySettings.codeTableCreated"));
    }
    codeTableDialogOpen.value = false;
    await loadCodeTables();
  } catch (e: unknown) {
    codeTableDialogError.value = formatError(
      e,
      "dataQualitySettings.codeTableSaveFailed"
    );
  } finally {
    codeTableSaving.value = false;
  }
}

function confirmDeleteCodeTable(table: QualityCodeTable) {
  codeTableToDelete.value = table;
  codeTableDeleteOpen.value = true;
}

/**
 * Delete one table. The server refuses one that a rule or template still
 * references (FailedPrecondition); that wording is surfaced in the card because
 * it names the checks that must be dealt with first.
 */
async function deleteCodeTable() {
  const table = codeTableToDelete.value;
  if (!table) {
    return;
  }
  codeTableDeleting.value = true;
  codeTableActionError.value = "";
  try {
    await deleteQualityCodeTable(table.name);
    notify.success(t("dataQualitySettings.codeTableDeleted"));
    codeTableDeleteOpen.value = false;
    codeTableToDelete.value = undefined;
    await loadCodeTables();
  } catch (e: unknown) {
    codeTableActionError.value = formatError(
      e,
      "dataQualitySettings.codeTableDeleteFailed"
    );
    codeTableDeleteOpen.value = false;
  } finally {
    codeTableDeleting.value = false;
  }
}

async function handleSave() {
  issues.value = validateSettingsForm(form.value);
  saveError.value = "";
  if (issues.value.length > 0) {
    return;
  }
  isSaving.value = true;
  try {
    await updateDataQualitySetting(formToSetting(form.value));
    notify.success(t("dataQualitySettings.saved"));
    await load();
  } catch (e: unknown) {
    saveError.value = formatError(e, "dataQualitySettings.saveFailed");
  } finally {
    isSaving.value = false;
  }
}

onMounted(() => {
  void load();
  void loadRunnerDatabases();
});
</script>
