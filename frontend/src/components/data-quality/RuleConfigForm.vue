<template>
  <div class="space-y-5">
    <p
      v-for="(hint, index) in oneOfHints"
      :key="index"
      class="rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground"
    >
      {{ t("dataQuality.schema.atLeastOneHint", { fields: hint }) }}
    </p>

    <!-- Shared by every threshold-carrying config: the unit the fail/warn values
         are read in, and the values themselves. custom SQL has no row total, so
         it keeps the fail/warn pair but never the unit. -->
    <div
      v-if="isCountRule || ruleType === QualityRuleType.CUSTOM_SQL"
      class="grid gap-4 sm:grid-cols-3"
    >
      <div
        v-if="isCountRule"
        class="flex flex-col gap-1.5"
      >
        <Label>{{ t("dataQuality.editor.thresholdMetric") }}</Label>
        <Select v-model="thresholdMetricModel">
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem :value="String(QualityThresholdMetric.COUNT)">
              {{ t("dataQuality.thresholdMetric.count") }}
            </SelectItem>
            <SelectItem :value="String(QualityThresholdMetric.PERCENT)">
              {{ t("dataQuality.thresholdMetric.percent") }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <FormField
        v-model="config.failThreshold"
        :id="fieldId('failThreshold')"
        :required="requiredFor('failThreshold')"
        :error="errorFor('failThreshold')"
        type="number"
        :label="t('dataQuality.editor.failThreshold')"
        :hint="
          isCountRule ? t('dataQuality.editor.thresholdHint') : undefined
        "
      />
      <FormField
        v-model="config.warnThreshold"
        :id="fieldId('warnThreshold')"
        :required="requiredFor('warnThreshold')"
        :error="errorFor('warnThreshold')"
        type="number"
        :label="t('dataQuality.editor.warnThreshold')"
      />
    </div>

    <template v-if="ruleType === QualityRuleType.ROW_COUNT">
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.minRows"
          :id="fieldId('minRows')"
          :required="requiredFor('minRows')"
          :error="errorFor('minRows')"
          type="number"
          :label="t('dataQuality.editor.minRows')"
        />
        <FormField
          v-model="config.maxRows"
          :id="fieldId('maxRows')"
          :required="requiredFor('maxRows')"
          :error="errorFor('maxRows')"
          type="number"
          :label="t('dataQuality.editor.maxRows')"
        />
        <FormField
          v-model="config.exactRows"
          :id="fieldId('exactRows')"
          :required="requiredFor('exactRows')"
          :error="errorFor('exactRows')"
          type="number"
          :label="t('dataQuality.editor.exactRows')"
        />
      </div>
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField
          v-model="config.warnMinRows"
          :id="fieldId('warnMinRows')"
          :required="requiredFor('warnMinRows')"
          :error="errorFor('warnMinRows')"
          type="number"
          :label="t('dataQuality.editor.warnMinRows')"
        />
        <FormField
          v-model="config.warnMaxRows"
          :id="fieldId('warnMaxRows')"
          :required="requiredFor('warnMaxRows')"
          :error="errorFor('warnMaxRows')"
          type="number"
          :label="t('dataQuality.editor.warnMaxRows')"
        />
      </div>
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.ROW_COUNT_CHANGE"
      class="grid gap-4 sm:grid-cols-2"
    >
      <FormField
        v-model="config.maxIncreasePct"
        :id="fieldId('maxIncreasePct')"
        :required="requiredFor('maxIncreasePct')"
        :error="errorFor('maxIncreasePct')"
        type="number"
        :label="t('dataQuality.editor.maxIncreasePct')"
        :hint="t('dataQuality.editor.percentHint')"
      />
      <FormField
        v-model="config.maxDecreasePct"
        :id="fieldId('maxDecreasePct')"
        :required="requiredFor('maxDecreasePct')"
        :error="errorFor('maxDecreasePct')"
        type="number"
        :label="t('dataQuality.editor.maxDecreasePct')"
        :hint="t('dataQuality.editor.percentHint')"
      />
      <FormField
        v-model="config.warnIncreasePct"
        :id="fieldId('warnIncreasePct')"
        :required="requiredFor('warnIncreasePct')"
        :error="errorFor('warnIncreasePct')"
        type="number"
        :label="t('dataQuality.editor.warnIncreasePct')"
      />
      <FormField
        v-model="config.warnDecreasePct"
        :id="fieldId('warnDecreasePct')"
        :required="requiredFor('warnDecreasePct')"
        :error="errorFor('warnDecreasePct')"
        type="number"
        :label="t('dataQuality.editor.warnDecreasePct')"
      />
    </div>

    <template v-else-if="ruleType === QualityRuleType.NULL_PERCENT">
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.nullColumn ?? ''"
            :columns="columnDetails"
            :id="fieldId('nullColumn')"
            :required="requiredFor('nullColumn')"
            :error="errorFor('nullColumn')"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('nullColumn', value)"
          />
        <div class="flex flex-col gap-1.5">
          <Label>{{ t("dataQuality.editor.nullMetric") }}</Label>
          <Select v-model="config.nullMetric">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="percent">
                {{ t("dataQuality.editor.nullMetricPercent") }}
              </SelectItem>
              <SelectItem value="count">
                {{ t("dataQuality.editor.nullMetricCount") }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      <div
        v-if="config.nullMetric === 'count'"
        class="grid gap-4 sm:grid-cols-2"
      >
        <FormField
          v-model="config.maxNullCount"
          :id="fieldId('maxNullCount')"
          :required="requiredFor('maxNullCount')"
          :error="errorFor('maxNullCount')"
          type="number"
          :label="t('dataQuality.editor.maxNullCount')"
        />
        <FormField
          v-model="config.warnNullCount"
          :id="fieldId('warnNullCount')"
          :required="requiredFor('warnNullCount')"
          :error="errorFor('warnNullCount')"
          type="number"
          :label="t('dataQuality.editor.warnNullCount')"
        />
      </div>
      <div
        v-else
        class="grid gap-4 sm:grid-cols-2"
      >
        <FormField
          v-model="config.maxNullPct"
          :id="fieldId('maxNullPct')"
          :required="requiredFor('maxNullPct')"
          :error="errorFor('maxNullPct')"
          type="number"
          :label="t('dataQuality.editor.maxNullPct')"
          :hint="t('dataQuality.editor.percentHint')"
        />
        <FormField
          v-model="config.warnNullPct"
          :id="fieldId('warnNullPct')"
          :required="requiredFor('warnNullPct')"
          :error="errorFor('warnNullPct')"
          type="number"
          :label="t('dataQuality.editor.warnNullPct')"
        />
      </div>
      <FormField
        v-model="config.nullValues"
        :id="fieldId('nullValues')"
        :required="requiredFor('nullValues')"
        :error="errorFor('nullValues')"
        :label="t('dataQuality.editor.nullValues')"
        :hint="t('dataQuality.editor.nullValuesHint')"
      />
    </template>

    <template v-else-if="ruleType === QualityRuleType.UNIQUE">
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.column ?? ''"
            :columns="columnDetails"
            :id="fieldId('column')"
            :required="requiredFor('column')"
            :error="errorFor('column')"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('column', value)"
          />
        <CheckboxField
          v-model="config.countNulls"
          :label="t('dataQuality.editor.countNulls')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.UNIQUE_COMBINATION">
      <ColumnSelect
        multiple
        :model-value="csvColumns(config.columns)"
        :columns="columnDetails"
        :id="fieldId('columns')"
        :required="requiredFor('columns')"
        :error="errorFor('columns')"
        :disabled="config.allColumns === true"
        :label="t('dataQuality.editor.columns')"
        :hint="t('dataQuality.editor.columnsHint')"
        @update:model-value="(value) => setCsvColumns('columns', value)"
      />
      <div class="grid gap-4 sm:grid-cols-2">
        <CheckboxField
          v-model="config.allColumns"
          :label="t('dataQuality.editor.allColumns')"
        />
        <CheckboxField
          v-model="config.countNulls"
          :label="t('dataQuality.editor.countNulls')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.ACCEPTED_VALUES">
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.column ?? ''"
            :columns="columnDetails"
            :id="fieldId('column')"
            :required="requiredFor('column')"
            :error="errorFor('column')"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('column', value)"
          />
        <CheckboxField
          v-model="config.allowNull"
          :label="t('dataQuality.editor.allowNull')"
        />
      </div>
      <FormField
        v-model="config.values"
        :id="fieldId('values')"
        :required="requiredFor('values')"
        :error="errorFor('values')"
        :label="t('dataQuality.editor.values')"
        :hint="t('dataQuality.editor.valuesHint')"
      />
    </template>

    <template v-else-if="ruleType === QualityRuleType.RANGE">
      <div class="grid gap-4 sm:grid-cols-3">
                  <ColumnSelect
            :model-value="config.column ?? ''"
            :columns="columnDetails"
            :id="fieldId('column')"
            :required="requiredFor('column')"
            :error="errorFor('column')"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('column', value)"
          />
        <FormField
          v-model="config.minValue"
          :id="fieldId('minValue')"
          :required="requiredFor('minValue')"
          :error="errorFor('minValue')"
          :label="t('dataQuality.editor.minValue')"
        />
        <FormField
          v-model="config.maxValue"
          :id="fieldId('maxValue')"
          :required="requiredFor('maxValue')"
          :error="errorFor('maxValue')"
          :label="t('dataQuality.editor.maxValue')"
        />
      </div>
      <CheckboxField
        v-model="config.inclusive"
        :label="t('dataQuality.editor.inclusive')"
      />
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.REGEX"
      class="grid gap-4 sm:grid-cols-2"
    >
      <ColumnSelect
        :model-value="config.column ?? ''"
        :columns="columnDetails"
        :id="fieldId('column')"
        :required="requiredFor('column')"
        :error="errorFor('column')"
        :label="t('dataQuality.editor.column')"
        :hint="t('dataQuality.editor.columnHint')"
        @update:model-value="(value) => setColumn('column', value)"
      />
      <FormField
        v-model="config.pattern"
        :id="fieldId('pattern')"
        :required="requiredFor('pattern')"
        :error="errorFor('pattern')"
        :label="t('dataQuality.editor.pattern')"
        :hint="t('dataQuality.editor.patternHint')"
      />
    </div>

    <div
      v-else-if="ruleType === QualityRuleType.STRING_LENGTH"
      class="grid gap-4 sm:grid-cols-3"
    >
      <ColumnSelect
        :model-value="config.column ?? ''"
        :columns="columnDetails"
        :id="fieldId('column')"
        :required="requiredFor('column')"
        :error="errorFor('column')"
        :label="t('dataQuality.editor.column')"
        :hint="t('dataQuality.editor.columnHint')"
        @update:model-value="(value) => setColumn('column', value)"
      />
      <FormField
        v-model="config.minLen"
        :id="fieldId('minLen')"
        :required="requiredFor('minLen')"
        :error="errorFor('minLen')"
        type="number"
        :label="t('dataQuality.editor.minLen')"
      />
      <FormField
        v-model="config.maxLen"
        :id="fieldId('maxLen')"
        :required="requiredFor('maxLen')"
        :error="errorFor('maxLen')"
        type="number"
        :label="t('dataQuality.editor.maxLen')"
      />
    </div>

    <template
      v-else-if="ruleType === QualityRuleType.REFERENTIAL_INTEGRITY"
    >
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.childColumn ?? ''"
            :columns="columnDetails"
            :id="fieldId('childColumn')"
            :required="requiredFor('childColumn')"
            :error="errorFor('childColumn')"
            :label="t('dataQuality.editor.childColumn')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('childColumn', value)"
          />
        <FormField
          v-model="config.parentTable"
          :id="fieldId('parentTable')"
          :required="requiredFor('parentTable')"
          :error="errorFor('parentTable')"
          :label="t('dataQuality.editor.parentTable')"
          :hint="t('dataQuality.editor.parentTableHint')"
        />
                  <ColumnSelect
            :model-value="config.parentColumn ?? ''"
            :columns="columnDetails"
            :id="fieldId('parentColumn')"
            :required="requiredFor('parentColumn')"
            :error="errorFor('parentColumn')"
            :label="t('dataQuality.editor.parentColumn')"
            @update:model-value="(value) => setColumn('parentColumn', value)"
          />
        <CheckboxField
          v-model="config.allowNull"
          :label="t('dataQuality.editor.allowNull')"
        />
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.CROSS_COLUMN">
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.leftColumn ?? ''"
            :columns="columnDetails"
            :id="fieldId('leftColumn')"
            :required="requiredFor('leftColumn')"
            :error="errorFor('leftColumn')"
            :label="t('dataQuality.editor.leftColumn')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('leftColumn', value)"
          />
        <div class="flex flex-col gap-1.5">
          <Label>{{ t("dataQuality.editor.crossOperator") }}</Label>
          <Select v-model="crossOperatorModel">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="operator in crossOperators"
                :key="operator"
                :value="String(operator)"
              >
                {{ t(qualityComparisonOperatorKey(operator)) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
                  <ColumnSelect
            :model-value="config.rightColumn ?? ''"
            :columns="columnDetails"
            :id="fieldId('rightColumn')"
            :required="requiredFor('rightColumn')"
            :error="errorFor('rightColumn')"
            :label="t('dataQuality.editor.rightColumn')"
            @update:model-value="(value) => setColumn('rightColumn', value)"
          />
        <FormField
          v-model="config.tolerance"
          :id="fieldId('tolerance')"
          :required="requiredFor('tolerance')"
          :error="errorFor('tolerance')"
          type="number"
          :label="t('dataQuality.editor.tolerance')"
          :hint="t('dataQuality.editor.toleranceHint')"
        />
      </div>
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.FRESHNESS"
      class="grid gap-4 sm:grid-cols-3"
    >
      <ColumnSelect
        :model-value="config.timestampColumn ?? ''"
        :columns="columnDetails"
        :id="fieldId('timestampColumn')"
        :required="requiredFor('timestampColumn')"
        :error="errorFor('timestampColumn')"
        :label="t('dataQuality.editor.timestampColumn')"
        :hint="t('dataQuality.editor.columnHint')"
        @update:model-value="(value) => setColumn('timestampColumn', value)"
      />
      <FormField
        v-model="config.maxLagSeconds"
        :id="fieldId('maxLagSeconds')"
        :required="requiredFor('maxLagSeconds')"
        :error="errorFor('maxLagSeconds')"
        type="number"
        :label="t('dataQuality.editor.maxLagSeconds')"
        :hint="t('dataQuality.editor.secondsHint')"
      />
      <FormField
        v-model="config.warnLagSeconds"
        :id="fieldId('warnLagSeconds')"
        :required="requiredFor('warnLagSeconds')"
        :error="errorFor('warnLagSeconds')"
        type="number"
        :label="t('dataQuality.editor.warnLagSeconds')"
      />
    </div>

    <template v-else-if="ruleType === QualityRuleType.CUSTOM_SQL">
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
          :aria-invalid="errorFor('sql') ? true : undefined"
          :class="errorFor('sql') ? 'border-destructive' : ''"
          :placeholder="t('dataQuality.editor.sqlPlaceholder')"
        />
        <p
          v-if="errorFor('sql')"
          class="text-sm text-destructive"
        >
          {{ errorFor("sql") }}
        </p>
        <p
          v-else
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQuality.editor.sqlHint") }}
        </p>
      </div>
    </template>

    <template v-else-if="ruleType === QualityRuleType.SCHEMA_DRIFT">
      <div
        id="dq-config-expected-columns"
        class="space-y-3"
        tabindex="-1"
      >
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div class="space-y-1">
            <Label>
              {{ t("dataQuality.editor.expectedColumns") }}
              <span class="text-destructive">*</span>
            </Label>
            <p
              v-if="errorFor('expectedColumns')"
              class="text-sm text-destructive"
            >
              {{ errorFor("expectedColumns") }}
            </p>
            <p
              v-else
              class="text-sm text-muted-foreground"
            >
              {{ t("dataQuality.editor.expectedColumnsHint") }}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            :disabled="columnDetails.length === 0"
            @click="fillExpectedColumns"
          >
            {{ t("dataQuality.editor.fillFromTarget") }}
          </Button>
        </div>
        <div
          v-for="(column, index) in config.expectedColumns ?? []"
          :key="index"
          class="grid items-center gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto_auto]"
        >
          <Input
            :model-value="column.name"
            :aria-label="t('dataQuality.editor.columnName')"
            :placeholder="t('dataQuality.editor.columnName')"
            @update:model-value="
              (value) => setExpectedColumnName(column, String(value))
            "
          />
          <Input
            v-model="column.type"
            :aria-label="t('dataQuality.editor.columnType')"
            :placeholder="t('dataQuality.editor.columnType')"
          />
          <CheckboxField
            :model-value="column.nullable === true"
            :label="t('dataQuality.editor.nullable')"
            @update:model-value="
              (value) => setExpectedColumnNullable(column, value)
            "
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            class="h-8 w-8 text-muted-foreground hover:text-destructive"
            :aria-label="t('dataQuality.editor.removeColumn')"
            :title="t('dataQuality.editor.removeColumn')"
            @click="removeExpectedColumn(index)"
          >
            <Trash2 class="h-4 w-4" />
          </Button>
        </div>
        <p
          v-if="(config.expectedColumns ?? []).length > 0"
          class="text-xs text-muted-foreground"
        >
          {{ t("dataQuality.editor.nullableHint") }}
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          @click="addExpectedColumn"
        >
          <Plus class="mr-1 h-4 w-4" />
          {{ t("dataQuality.editor.addColumn") }}
        </Button>
        <div class="grid gap-4 sm:grid-cols-3">
          <CheckboxField
            v-model="config.checkTypes"
            :label="t('dataQuality.editor.checkTypes')"
          />
          <CheckboxField
            v-model="config.checkOrder"
            :label="t('dataQuality.editor.checkOrder')"
          />
          <CheckboxField
            v-model="config.allowExtraColumns"
            :label="t('dataQuality.editor.allowExtraColumns')"
          />
        </div>
        <div class="grid gap-4 sm:grid-cols-2">
          <FormField
            v-model="config.failThreshold"
            :id="fieldId('failThreshold')"
            :required="requiredFor('failThreshold')"
            :error="errorFor('failThreshold')"
            type="number"
            :label="t('dataQuality.editor.failThreshold')"
          />
          <FormField
            v-model="config.warnThreshold"
            :id="fieldId('warnThreshold')"
            :required="requiredFor('warnThreshold')"
            :error="errorFor('warnThreshold')"
            type="number"
            :label="t('dataQuality.editor.warnThreshold')"
          />
        </div>
      </div>
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.GROUP_CHECK"
      class="grid gap-4 sm:grid-cols-3"
    >
      <ColumnSelect
        multiple
        :model-value="csvColumns(config.groupColumns)"
        :columns="columnDetails"
        :id="fieldId('groupColumns')"
        :required="requiredFor('groupColumns')"
        :error="errorFor('groupColumns')"
        :label="t('dataQuality.editor.groupColumns')"
        :hint="t('dataQuality.editor.groupColumnsHint')"
        @update:model-value="(value) => setCsvColumns('groupColumns', value)"
      />
      <FormField
        v-model="config.minRowsPerGroup"
        :id="fieldId('minRowsPerGroup')"
        :required="requiredFor('minRowsPerGroup')"
        :error="errorFor('minRowsPerGroup')"
        type="number"
        :label="t('dataQuality.editor.minRowsPerGroup')"
      />
      <FormField
        v-model="config.maxRowsPerGroup"
        :id="fieldId('maxRowsPerGroup')"
        :required="requiredFor('maxRowsPerGroup')"
        :error="errorFor('maxRowsPerGroup')"
        type="number"
        :label="t('dataQuality.editor.maxRowsPerGroup')"
      />
    </div>

    <template v-else-if="ruleType === QualityRuleType.AGGREGATE">
      <div class="grid gap-4 sm:grid-cols-2">
                  <ColumnSelect
            :model-value="config.aggregateColumn ?? ''"
            :columns="columnDetails"
            :id="fieldId('aggregateColumn')"
            :required="requiredFor('aggregateColumn')"
            :error="errorFor('aggregateColumn')"
            :label="t('dataQuality.editor.column')"
            :hint="t('dataQuality.editor.columnHint')"
            @update:model-value="(value) => setColumn('aggregateColumn', value)"
          />
        <div class="flex flex-col gap-1.5">
          <Label>{{ t("dataQuality.editor.aggregateFunction") }}</Label>
          <Select v-model="aggregateFunctionModel">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem
                v-for="fn in AGGREGATE_FUNCTIONS"
                :key="fn"
                :value="String(fn)"
              >
                {{ t(qualityAggregateFunctionKey(fn)) }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <FormField
          v-if="aggregateFunctionModel === String(QualityAggregateFunction.PERCENTILE)"
          v-model="config.percentile"
          type="number"
          :label="t('dataQuality.editor.percentile')"
          :hint="t('dataQuality.editor.percentileHint')"
        />
        <FormField
          v-model="config.aggMin"
          :id="fieldId('aggMin')"
          :required="requiredFor('aggMin')"
          :error="errorFor('aggMin')"
          type="number"
          :label="t('dataQuality.editor.minValue')"
        />
        <FormField
          v-model="config.aggMax"
          :id="fieldId('aggMax')"
          :required="requiredFor('aggMax')"
          :error="errorFor('aggMax')"
          type="number"
          :label="t('dataQuality.editor.maxValue')"
        />
        <FormField
          v-model="config.aggWarnMin"
          :id="fieldId('aggWarnMin')"
          :required="requiredFor('aggWarnMin')"
          :error="errorFor('aggWarnMin')"
          type="number"
          :label="t('dataQuality.editor.warnMin')"
        />
        <FormField
          v-model="config.aggWarnMax"
          :id="fieldId('aggWarnMax')"
          :required="requiredFor('aggWarnMax')"
          :error="errorFor('aggWarnMax')"
          type="number"
          :label="t('dataQuality.editor.warnMax')"
        />
      </div>
      <CheckboxField
        v-model="config.aggInclusive"
        :label="t('dataQuality.editor.inclusive')"
      />
    </template>

    <div
      v-else-if="ruleType === QualityRuleType.CROSS_TABLE"
      class="grid gap-4 sm:grid-cols-2"
    >
      <FormField
        v-model="config.rightTable"
        :id="fieldId('rightTable')"
        :required="requiredFor('rightTable')"
        :error="errorFor('rightTable')"
        class="sm:col-span-2"
        :label="t('dataQuality.editor.rightTable')"
        :hint="t('dataQuality.editor.rightTableHint')"
      />
      <div class="flex flex-col gap-1.5">
        <Label>{{ t("dataQuality.editor.leftAggregate") }}</Label>
        <Select
          :model-value="String(config.leftAggFunction ?? QualityAggregateFunction.COUNT)"
          @update:model-value="setLeftFunction"
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="fn in CROSS_TABLE_FUNCTIONS"
              :key="fn"
              :value="String(fn)"
            >
              {{ t(qualityAggregateFunctionKey(fn)) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <ColumnSelect
        :model-value="config.leftAggColumn ?? ''"
        :columns="columnDetails"
        :id="fieldId('leftAggColumn')"
        :required="requiredFor('leftAggColumn')"
        :error="errorFor('leftAggColumn')"
        :disabled="leftAggregateIsCount"
        :label="t('dataQuality.editor.leftColumn')"
        :hint="leftAggregateIsCount ? t('dataQuality.editor.countNeedsNoColumn') : t('dataQuality.editor.columnHint')"
        @update:model-value="(value) => setColumn('leftAggColumn', value)"
      />
      <div class="flex flex-col gap-1.5">
        <Label>{{ t("dataQuality.editor.rightAggregate") }}</Label>
        <Select
          :model-value="String(config.rightAggFunction ?? QualityAggregateFunction.COUNT)"
          @update:model-value="setRightFunction"
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem
              v-for="fn in CROSS_TABLE_FUNCTIONS"
              :key="fn"
              :value="String(fn)"
            >
              {{ t(qualityAggregateFunctionKey(fn)) }}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
      <ColumnSelect
        :model-value="config.rightAggColumn ?? ''"
        :columns="columnDetails"
        :id="fieldId('rightAggColumn')"
        :required="requiredFor('rightAggColumn')"
        :error="errorFor('rightAggColumn')"
        :disabled="rightAggregateIsCount"
        :label="t('dataQuality.editor.rightColumn')"
        :hint="rightAggregateIsCount ? t('dataQuality.editor.countNeedsNoColumn') : t('dataQuality.editor.rightColumnHint')"
        @update:model-value="(value) => setColumn('rightAggColumn', value)"
      />
      <FormField
        v-model="config.tolerance"
        :id="fieldId('tolerance')"
        :required="requiredFor('tolerance')"
        :error="errorFor('tolerance')"
        type="number"
        class="sm:col-span-2"
        :label="t('dataQuality.editor.tolerance')"
        :hint="t('dataQuality.editor.toleranceHint')"
      />
      <FormField
        v-model="config.failThreshold"
        :id="fieldId('failThreshold')"
        :required="requiredFor('failThreshold')"
        :error="errorFor('failThreshold')"
        type="number"
        :label="t('dataQuality.editor.failThreshold')"
      />
      <FormField
        v-model="config.warnThreshold"
        :id="fieldId('warnThreshold')"
        :required="requiredFor('warnThreshold')"
        :error="errorFor('warnThreshold')"
        type="number"
        :label="t('dataQuality.editor.warnThreshold')"
      />
    </div>
    <template v-else-if="ruleType === QualityRuleType.CEL_EXPRESSION">
      <div class="space-y-2">
        <Label for="dq-config-cel">
          {{ t("dataQuality.editor.celExpression") }}
          <span class="text-destructive">*</span>
        </Label>
        <textarea
          id="dq-config-cel"
          v-model="config.celExpression"
          class="min-h-48 w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm"
          :aria-required="true"
          :aria-invalid="errorFor('celExpression') ? true : undefined"
          :class="errorFor('celExpression') ? 'border-destructive' : ''"
          :placeholder="t('dataQuality.editor.celExpressionPlaceholder')"
        />
        <p
          v-if="errorFor('celExpression')"
          class="text-sm text-destructive"
        >
          {{ errorFor("celExpression") }}
        </p>
        <p
          v-else
          class="text-sm text-muted-foreground"
        >
          {{ t("dataQuality.editor.celExpressionHint") }}
        </p>
      </div>
      <div class="grid gap-4 sm:grid-cols-2">
        <ColumnSelect
          multiple
          :model-value="csvColumns(config.celColumns)"
          :columns="columnDetails"
          :id="fieldId('celColumns')"
          :required="requiredFor('celColumns')"
          :error="errorFor('celColumns')"
          :label="t('dataQuality.editor.celColumns')"
          :hint="t('dataQuality.editor.celColumnsHint')"
          @update:model-value="(value) => setCsvColumns('celColumns', value)"
        />
        <FormField
          v-model="config.celSampleRows"
          :id="fieldId('celSampleRows')"
          :required="requiredFor('celSampleRows')"
          :error="errorFor('celSampleRows')"
          type="number"
          :label="t('dataQuality.editor.celSampleRows')"
          :hint="t('dataQuality.editor.celSampleRowsHint')"
        />
      </div>
      <p class="text-xs text-muted-foreground">
        {{ t("dataQuality.editor.celFunctionsHint") }}
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { Plus, Trash2 } from "lucide-vue-next";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import { Button } from "@/components/ui/button";
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
  QualityAggregateFunction,
  QualityComparisonOperator,
  QualityRuleType,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  qualityAggregateFunctionKey,
  qualityComparisonOperatorKey,
} from "@/utils/dataQuality";
import {
  AGGREGATE_FUNCTIONS,
  CROSS_TABLE_FUNCTIONS,
  isThresholdMetricType,
  type RuleConfigForm,
  type SchemaDriftColumnForm,
  type TargetColumnDetail,
} from "@/utils/dataQualityForm";
import { isFieldRequired, oneOfGroups } from "@/utils/dataQualitySchema";
import CheckboxField from "./CheckboxField.vue";

/**
 * The config fields of one rule type. The component is a controlled editor for
 * the nested `RuleConfigForm` object: the parent owns the state and the reset on
 * a rule-type change, so a stale arm can never travel with a new type.
 */

const props = withDefaults(
  defineProps<{
    ruleType: QualityRuleType;
    /** Used by the schema-drift snapshot's "fill from target" action. */
    columnDetails?: TargetColumnDetail[];
    /** Inline errors keyed by config field, from the editor's pre-flight check. */
    fieldErrors?: Record<string, string>;
  }>(),
  { columnDetails: () => [], fieldErrors: () => ({}) }
);

const config = defineModel<RuleConfigForm>({ required: true });
const thresholdMetric = defineModel<QualityThresholdMetric>("thresholdMetric", {
  required: true,
});

const { t } = useI18n();

function fieldId(field: string): string {
  return `dq-field-${field}`;
}

function requiredFor(field: string): boolean {
  return isFieldRequired(props.ruleType, field);
}

function errorFor(field: string): string {
  return props.fieldErrors[field] ?? "";
}

/** One line per "at least one of" group, so the group's rule is visible. */
const oneOfHints = computed(() =>
  [...oneOfGroups(props.ruleType).values()].map((fields) =>
    fields.map((field) => t(field.labelKey)).join(" / ")
  )
);

/** A CSV column field as the picker's array form. */
function csvColumns(value: string | undefined): string[] {
  return (value ?? "")
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
}

/** The picker's array back into the CSV field the config builder parses. */
function setCsvColumns(
  field: "columns" | "groupColumns" | "celColumns",
  value: string | string[]
) {
  config.value[field] = Array.isArray(value) ? value.join(", ") : value;
}

function setColumn(
  field:
    | "nullColumn"
    | "column"
    | "childColumn"
    | "parentColumn"
    | "leftColumn"
    | "rightColumn"
    | "timestampColumn"
    | "aggregateColumn"
    | "leftAggColumn"
    | "rightAggColumn",
  value: string | string[]
) {
  config.value[field] = String(value);
}

const isCountRule = computed(() => isThresholdMetricType(props.ruleType));

const crossOperators = [
  QualityComparisonOperator.EQ,
  QualityComparisonOperator.NE,
  QualityComparisonOperator.LT,
  QualityComparisonOperator.LTE,
  QualityComparisonOperator.GT,
  QualityComparisonOperator.GTE,
];

const crossOperatorModel = computed({
  get: () => String(config.value.crossOperator ?? QualityComparisonOperator.EQ),
  set: (value: string) => {
    config.value.crossOperator = Number(value) as QualityComparisonOperator;
  },
});

const thresholdMetricModel = computed({
  get: () => String(thresholdMetric.value),
  set: (value: string) => {
    thresholdMetric.value = Number(value) as QualityThresholdMetric;
  },
});

const aggregateFunctionModel = computed({
  get: () =>
    String(config.value.aggregateFunction ?? QualityAggregateFunction.SUM),
  set: (value: string) => {
    config.value.aggregateFunction = Number(value) as QualityAggregateFunction;
  },
});

const leftAggregateIsCount = computed(
  () =>
    (config.value.leftAggFunction ?? QualityAggregateFunction.COUNT) ===
    QualityAggregateFunction.COUNT
);
const rightAggregateIsCount = computed(
  () =>
    (config.value.rightAggFunction ?? QualityAggregateFunction.COUNT) ===
    QualityAggregateFunction.COUNT
);

// COUNT takes no column. Clearing it when the function becomes COUNT keeps the
// form and the compiled request in agreement; the server also rejects a stray.
function setLeftFunction(value: unknown) {
  const fn = Number(value) as QualityAggregateFunction;
  config.value.leftAggFunction = fn;
  if (fn === QualityAggregateFunction.COUNT) {
    config.value.leftAggColumn = "";
  }
}

function setRightFunction(value: unknown) {
  const fn = Number(value) as QualityAggregateFunction;
  config.value.rightAggFunction = fn;
  if (fn === QualityAggregateFunction.COUNT) {
    config.value.rightAggColumn = "";
  }
}

function addExpectedColumn() {
  config.value.expectedColumns = [
    ...(config.value.expectedColumns ?? []),
    // Nullability starts unset: the rule should not judge it until a target
    // column or the reader supplies the expected bit.
    { name: "", type: "" },
  ];
}

/**
 * A manual expected-column row borrows the target's own description when the
 * name matches: type when the row left it blank, and the explicit nullability
 * either way. This is the same contract "fill from target" records, one row at
 * a time.
 */
function setExpectedColumnName(column: SchemaDriftColumnForm, value: string) {
  column.name = value;
  const match = props.columnDetails.find(
    (detail) => detail.name === value.trim()
  );
  if (!match) {
    return;
  }
  if (column.type.trim() === "") {
    column.type = match.type;
  }
  column.nullable = match.nullable;
}

/** Writes the snapshot bit only once the reader toggles the checkbox. */
function setExpectedColumnNullable(
  column: SchemaDriftColumnForm,
  value: boolean
) {
  column.nullable = value;
}

function removeExpectedColumn(index: number) {
  config.value.expectedColumns = (config.value.expectedColumns ?? []).filter(
    (_, position) => position !== index
  );
}

function fillExpectedColumns() {
  config.value.expectedColumns = props.columnDetails.map(
    (column): SchemaDriftColumnForm => ({
      name: column.name,
      type: column.type,
      nullable: column.nullable,
    })
  );
}
</script>
