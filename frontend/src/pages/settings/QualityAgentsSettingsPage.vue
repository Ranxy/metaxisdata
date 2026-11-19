<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('qualityAgents.title')"
      :description="t('qualityAgents.description')"
    >
      <template #actions>
        <Button
          v-if="canManage"
          :disabled="loading"
          @click="openCreate"
        >
          <Plus class="mr-2 h-4 w-4" />
          {{ t("qualityAgents.create") }}
        </Button>
        <Button
          variant="outline"
          :disabled="loading"
          @click="load"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': loading }"
          />
          {{ t("common.refresh") }}
        </Button>
      </template>
    </PageHeader>

    <Alert
      v-if="error"
      variant="destructive"
    >
      <AlertTriangle class="h-4 w-4" />
      <AlertDescription class="break-words">{{ error }}</AlertDescription>
    </Alert>

    <Card>
      <CardHeader>
        <CardTitle>{{ t("qualityAgents.workspaceSection") }}</CardTitle>
        <CardDescription>{{ t("qualityAgents.workspaceSectionHint") }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-3">
        <div class="flex items-start gap-3">
          <Checkbox
            id="qa-runner-enabled"
            :checked="!switches.runnerEnabled"
            :disabled="!canManage || savingSwitch"
            @update:checked="saveSwitch('runner', $event !== true)"
          />
          <div>
            <Label for="qa-runner-enabled">{{ t("qualityAgents.pauseAll") }}</Label>
            <p class="text-sm text-muted-foreground">{{ t("qualityAgents.pauseAllHint") }}</p>
          </div>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <CardTitle>{{ t("qualityAgents.listSection") }}</CardTitle>
        <CardDescription>{{ t("qualityAgents.listSectionHint") }}</CardDescription>
      </CardHeader>
      <CardContent>
        <PageState :loading="loading">
          <div
            v-if="agents.length === 0"
            class="rounded-lg border border-dashed p-8 text-center"
          >
            <p class="font-medium">{{ t("qualityAgents.emptyTitle") }}</p>
            <p class="mt-1 text-sm text-muted-foreground">{{ t("qualityAgents.emptyDescription") }}</p>
          </div>
          <Table v-else>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("qualityAgents.columnName") }}</TableHead>
                <TableHead>{{ t("qualityAgents.columnOwner") }}</TableHead>
                <TableHead>{{ t("qualityAgents.columnCapabilities") }}</TableHead>
                <TableHead>{{ t("qualityAgents.columnDatabases") }}</TableHead>
                <TableHead>{{ t("qualityAgents.columnState") }}</TableHead>
                <TableHead class="text-right">{{ t("common.actions") }}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="agent in agents"
                :key="agent.name"
              >
                <TableCell>
                  <div class="flex flex-col">
                    <span class="font-medium">{{ agent.displayName }}</span>
                    <span class="text-xs text-muted-foreground">{{ agent.description }}</span>
                    <span class="text-xs text-muted-foreground">
                      {{ agentProviderLabel(agent) }}
                    </span>
                  </div>
                </TableCell>
                <TableCell>{{ ownerLabel(agent.owner) }}</TableCell>
                <TableCell>
                  <div class="flex flex-wrap gap-1">
                    <Badge
                      v-for="capability in agent.capabilities"
                      :key="capability"
                      variant="secondary"
                    >
                      {{ capabilityLabel(capability) }}
                    </Badge>
                  </div>
                </TableCell>
                <TableCell>{{ agent.databases.length }}</TableCell>
                <TableCell>
                  <Badge :variant="stateVariant(agent.state)">
                    {{ stateLabel(agent.state) }}
                  </Badge>
                </TableCell>
                <TableCell class="text-right">
                  <div class="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      as-child
                    >
                      <RouterLink :to="agentHomeRoute(agent.name)">
                        {{ t("qualityAgents.open") }}
                      </RouterLink>
                    </Button>
                    <Button
                      v-if="canManage"
                      variant="ghost"
                      size="sm"
                      @click="openEdit(agent)"
                    >
                      {{ t("qualityAgents.edit") }}
                    </Button>
                    <Button
                      v-if="canManage && agent.state === QualityAgentState.ACTIVE"
                      variant="ghost"
                      size="sm"
                      :disabled="busyAgent === agent.name"
                      @click="pause(agent)"
                    >
                      {{ t("qualityAgents.pause") }}
                    </Button>
                    <Button
                      v-if="canManage && (agent.state === QualityAgentState.PAUSED || agent.state === QualityAgentState.DRAFT)"
                      variant="ghost"
                      size="sm"
                      :disabled="busyAgent === agent.name"
                      @click="start(agent)"
                    >
                      {{ t("qualityAgents.start") }}
                    </Button>
                    <Button
                      v-if="canManage"
                      variant="ghost"
                      size="sm"
                      class="text-destructive"
                      :disabled="busyAgent === agent.name"
                      @click="disable(agent)"
                    >
                      {{ t("qualityAgents.disable") }}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </PageState>
      </CardContent>
    </Card>

    <Dialog
      :open="createOpen"
      @update:open="createOpen = $event"
    >
      <DialogContent class="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {{ editingAgent ? t("qualityAgents.editTitle") : t("qualityAgents.createTitle") }}
          </DialogTitle>
          <DialogDescription>
            {{ editingAgent ? t("qualityAgents.editDescription") : t("qualityAgents.createDescription") }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4">
          <div class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-1">
              <Label for="qa-name">{{ t("qualityAgents.name") }}</Label>
              <Input
                id="qa-name"
                v-model="form.displayName"
                maxlength="200"
              />
            </div>
            <div class="space-y-1">
              <Label for="qa-owner">{{ t("qualityAgents.owner") }}</Label>
              <Select v-model="form.owner">
                <SelectTrigger id="qa-owner">
                  <SelectValue :placeholder="t('qualityAgents.ownerPlaceholder')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="user in users"
                    :key="user.name"
                    :value="user.name"
                  >
                    {{ user.email }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-1">
              <Label for="qa-description">{{ t("qualityAgents.descriptionLabel") }}</Label>
              <Input
                id="qa-description"
                v-model="form.description"
                maxlength="2000"
              />
            </div>
            <div class="space-y-1">
              <Label for="qa-language">{{ t("qualityAgents.language") }}</Label>
              <Select v-model="form.language">
                <SelectTrigger id="qa-language">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="follow">{{ t("qualityAgents.languageFollowOwner") }}</SelectItem>
                  <SelectItem value="en-US">{{ t("qualityAgents.languageEnglish") }}</SelectItem>
                  <SelectItem value="zh-CN">{{ t("qualityAgents.languageChinese") }}</SelectItem>
                </SelectContent>
              </Select>
              <p class="text-xs text-muted-foreground">
                {{ t("qualityAgents.languageHint") }}
              </p>
            </div>
          </div>

          <div class="grid gap-3 sm:grid-cols-2">
            <div class="space-y-1">
              <Label for="qa-provider">{{ t("qualityAgents.providerProfile") }}</Label>
              <Select
                :model-value="form.providerProfile"
                :disabled="providerOptions.length === 0"
                @update:model-value="setProviderProfile"
              >
                <SelectTrigger id="qa-provider">
                  <SelectValue :placeholder="t('qualityAgents.providerProfilePlaceholder')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-if="providerUnavailable"
                    :value="form.providerProfile"
                  >
                    {{ unavailableProviderLabel }}
                  </SelectItem>
                  <SelectItem
                    v-for="option in providerOptions"
                    :key="option.name"
                    :value="option.name"
                  >
                    {{ option.title }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p class="text-xs text-muted-foreground">
                {{ profilesLoaded && providerOptions.length === 0 ? t("qualityAgents.providerNoProfiles") : t("qualityAgents.providerProfileHint") }}
              </p>
            </div>
            <div class="space-y-1">
              <Label for="qa-model">{{ t("qualityAgents.providerModel") }}</Label>
              <Select
                :model-value="form.providerModel"
                :disabled="!form.providerProfile || modelOptions.length === 0"
                @update:model-value="form.providerModel = String($event)"
              >
                <SelectTrigger id="qa-model">
                  <SelectValue :placeholder="t('qualityAgents.providerModelPlaceholder')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-if="modelUnavailable"
                    :value="form.providerModel"
                  >
                    {{ t("qualityAgents.providerUnavailableOption", { name: form.providerModel }) }}
                  </SelectItem>
                  <SelectItem
                    v-for="model in modelOptions"
                    :key="model"
                    :value="model"
                  >
                    {{ model }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <p class="text-xs text-muted-foreground">
                {{ t("qualityAgents.providerModelHint") }}
              </p>
            </div>
          </div>
          <p
            v-if="profilesError"
            class="text-xs text-warning"
          >
            {{ t("qualityAgents.providerProfilesError") }}
          </p>
          <p
            v-else-if="providerUnavailable || modelUnavailable"
            class="text-xs text-warning"
          >
            {{ t("qualityAgents.providerUnavailableHint") }}
          </p>
          <p
            v-else-if="!form.providerProfile || !form.providerModel"
            class="text-xs text-muted-foreground"
          >
            {{ t("qualityAgents.providerRequiredHint") }}
          </p>
          <p
            v-if="dataQualityLoaded && dataQuality.enableLlmAnalysis === false"
            class="text-xs text-muted-foreground"
          >
            {{ t("qualityAgents.providerAnalysisOff") }}
          </p>

          <div class="space-y-2">
            <Label>{{ t("qualityAgents.capabilities") }}</Label>
            <div class="flex flex-wrap gap-4">
              <label
                v-for="capability in capabilities"
                :key="capability.id"
                class="flex items-start gap-2 text-sm"
              >
                <Checkbox
                  :checked="form.capabilities.includes(capability.id)"
                  @update:checked="toggleCapability(capability.id, $event === true)"
                />
                <span>
                  <span class="font-medium">{{ capability.title }}</span>
                  <span class="block text-xs text-muted-foreground">{{ capability.description }}</span>
                </span>
              </label>
            </div>
          </div>

          <div
            v-if="form.capabilities.includes('patrol')"
            class="grid gap-3 rounded-md border p-3 sm:grid-cols-3"
          >
            <div class="space-y-1">
              <Label>{{ t("qualityAgents.reportFrequency") }}</Label>
              <Select v-model="form.reportFrequency">
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">{{ t("qualityAgents.reportOff") }}</SelectItem>
                  <SelectItem value="daily">{{ t("qualityAgents.reportDaily") }}</SelectItem>
                  <SelectItem value="weekly">{{ t("qualityAgents.reportWeekly") }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="space-y-1">
              <Label>{{ t("qualityAgents.reportTime") }}</Label>
              <Input
                v-model="form.reportTime"
                placeholder="09:00"
              />
            </div>
            <div class="space-y-1">
              <Label>{{ t("qualityAgents.reportTimezone") }}</Label>
              <Input
                v-model="form.reportTimezone"
                placeholder="Asia/Shanghai"
              />
            </div>
            <div
              v-if="form.reportFrequency === 'weekly'"
              class="space-y-1"
            >
              <Label>{{ t("qualityAgents.reportWeekday") }}</Label>
              <Select v-model="form.reportWeekday">
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="weekday in weekdays"
                    :key="weekday.value"
                    :value="weekday.value"
                  >
                    {{ t(weekday.key) }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div
            v-if="form.capabilities.includes('adopt')"
            class="space-y-3 rounded-md border p-3"
          >
            <div class="grid gap-3 sm:grid-cols-2">
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.adoptionMode") }}</Label>
                <Select v-model="form.adoptionMode">
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="off">{{ t("qualityAgents.adoptionOff") }}</SelectItem>
                    <SelectItem value="paused">{{ t("qualityAgents.adoptionPaused") }}</SelectItem>
                    <SelectItem value="active">{{ t("qualityAgents.adoptionActive") }}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.adoptionMaxPerDay") }}</Label>
                <Input
                  v-model.number="form.adoptionMaxPerDay"
                  inputmode="numeric"
                />
              </div>
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.adoptionConfidence") }}</Label>
                <Input
                  v-model.number="form.adoptionConfidence"
                  inputmode="decimal"
                />
              </div>
              <label class="flex items-center gap-2 pt-6 text-sm">
                <Checkbox
                  :checked="form.adoptionAllowCritical"
                  @update:checked="form.adoptionAllowCritical = $event === true"
                />
                {{ t("qualityAgents.adoptionAllowCritical") }}
              </label>
            </div>
            <div class="space-y-2">
              <Label>{{ t("qualityAgents.adoptionRuleTypes") }}</Label>
              <p class="text-xs text-muted-foreground">{{ t("qualityAgents.adoptionRuleTypesHint") }}</p>
              <div class="flex flex-wrap gap-3">
                <label
                  v-for="ruleType in lowRiskRuleTypes"
                  :key="ruleType"
                  class="flex items-center gap-2 text-sm"
                >
                  <Checkbox
                    :checked="form.adoptionRuleTypes.includes(ruleType)"
                    @update:checked="toggleAdoptionRuleType(ruleType, $event === true)"
                  />
                  {{ t(qualityRuleTypeKey(ruleType)) }}
                </label>
              </div>
            </div>
          </div>

          <div class="space-y-2">
            <Label>{{ t("qualityAgents.databases") }}</Label>
            <p class="text-xs text-muted-foreground">{{ t("qualityAgents.databasesHint") }}</p>
            <div class="max-h-56 space-y-2 overflow-y-auto rounded-md border p-3">
              <p
                v-if="databasesLoading"
                class="text-sm text-muted-foreground"
              >
                {{ t("common.loading") }}
              </p>
              <p
                v-else-if="databaseGroups.length === 0"
                class="text-sm text-muted-foreground"
              >
                {{ t("qualityAgents.noDatabases") }}
              </p>
              <div
                v-for="group in databaseGroups"
                :key="group.instance"
              >
                <p class="mb-1 text-xs font-medium text-muted-foreground">{{ group.title }}</p>
                <label
                  v-for="database in group.databases"
                  :key="database.name"
                  class="flex items-center gap-2 py-0.5 text-sm"
                >
                  <Checkbox
                    :checked="form.databases.includes(database.name)"
                    @update:checked="toggleDatabase(database.name, $event === true)"
                  />
                  <span>{{ databaseName(database.name) }}</span>
                  <Badge
                    v-if="overlap(database.name).length > 0"
                    variant="warning"
                    class="ml-auto"
                  >
                    {{ t("qualityAgents.overlapBadge", { agents: overlap(database.name).join(", ") }) }}
                  </Badge>
                </label>
              </div>
            </div>
            <label
              v-if="hasOverlap"
              class="flex items-center gap-2 text-sm text-warning"
            >
              <Checkbox
                :checked="overlapAcknowledged"
                @update:checked="overlapAcknowledged = $event === true"
              />
              {{ t("qualityAgents.overlapAcknowledge") }}
            </label>
          </div>

          <div class="space-y-2 rounded-md border p-3">
            <Label>{{ t("qualityAgents.budgetSection") }}</Label>
            <p class="text-xs text-muted-foreground">{{ t("qualityAgents.budgetHint") }}</p>
            <div class="grid gap-3 sm:grid-cols-3">
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.budgetTokens") }}</Label>
                <Input v-model.number="form.budgetTokensPerHour" inputmode="numeric" />
              </div>
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.budgetWakes") }}</Label>
                <Input v-model.number="form.budgetWakesPerHour" inputmode="numeric" />
              </div>
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.budgetConcurrency") }}</Label>
                <Input v-model.number="form.budgetMaxConcurrentSessions" inputmode="numeric" />
              </div>
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.budgetNewSessions") }}</Label>
                <Input v-model.number="form.budgetMaxNewSessionsPerHour" inputmode="numeric" />
              </div>
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.budgetActiveSessions") }}</Label>
                <Input v-model.number="form.budgetMaxActiveSessions" inputmode="numeric" />
              </div>
              <div class="space-y-1">
                <Label>{{ t("qualityAgents.budgetConversation") }}</Label>
                <Input v-model.number="form.budgetConversationMessagesPerHour" inputmode="numeric" />
              </div>
            </div>
          </div>

          <Alert
            v-if="createError"
            variant="destructive"
          >
            <AlertDescription>{{ createError }}</AlertDescription>
          </Alert>

          <div class="flex items-center justify-between gap-3">
            <label class="flex items-center gap-2 text-sm">
              <Checkbox
                :checked="form.start"
                @update:checked="form.start = $event === true"
              />
              {{ t("qualityAgents.startNow") }}
            </label>
            <div class="flex gap-2">
              <Button
                variant="outline"
                :disabled="creating"
                @click="createOpen = false"
              >
                {{ t("common.cancel") }}
              </Button>
              <Button
                :disabled="creating || !canCreate"
                @click="submitCreate"
              >
                <Loader2
                  v-if="creating"
                  class="mr-2 h-4 w-4 animate-spin"
                />
                {{ editingAgent ? t("qualityAgents.saveAction") : t("qualityAgents.createAction") }}
              </Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { AlertTriangle, Loader2, Plus, RefreshCcw } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  createQualityAgent,
  disableQualityAgent,
  listQualityAgents,
  pauseQualityAgent,
  type QualityAgentConfigInput,
  startQualityAgent,
  updateQualityAgent,
} from "@/api/data-quality";
import { listDatabases } from "@/api/database";
import { listAll } from "@/api/list";
import { listProfiles } from "@/api/llm";
import {
  getWorkspaceProfileSetting,
  updateDataQualitySetting,
} from "@/api/setting";
import { listAllUsers } from "@/api/user";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
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
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
  type QualityAgent,
  QualityAgentAdoptionMode,
  QualityAgentAdoptionSettingSchema,
  QualityAgentBudgetSchema,
  QualityAgentReportFrequency,
  QualityAgentReportScheduleSchema,
  QualityAgentSchema,
  QualityAgentState,
  QualityRuleType,
} from "@/types/proto-es/v1/data_quality_service_pb";
import type { LlmProviderProfile } from "@/types/proto-es/v1/llm_service_pb";
import type { DataQualitySetting } from "@/types/proto-es/v1/setting_service_pb";
import { DataQualitySettingSchema } from "@/types/proto-es/v1/setting_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { qualityRuleTypeKey } from "@/utils/dataQuality";
import {
  buildProviderOptions,
  enabledModelsFor,
  type ProviderOption,
  profileTitle,
} from "@/utils/llmProvider";
import { qualityAgentRoute } from "@/utils/qualityAgent";

const { t } = useI18n();
const { formatError } = useErrorMessage();
const authStore = useAuthStore();

const canManage = computed(() =>
  authStore.hasPermission("metaxisdata.dataQuality.agents.manage")
);

const agents = ref<QualityAgent[]>([]);
const users = ref<User[]>([]);
const loading = ref(true);
const error = ref("");
const savingSwitch = ref(false);
const busyAgent = ref("");
const switches = ref({ runnerEnabled: false });
const dataQuality = ref<DataQualitySetting>(
  create(DataQualitySettingSchema, {})
);
// The raw profiles feed the model select; the options are the allowed profiles that
// enable a model, so the dialog cannot offer a profile the agent could not run on.
const providerProfiles = ref<LlmProviderProfile[]>([]);
const providerOptions = ref<ProviderOption[]>([]);
// "Loaded but empty" and "could not load" mean different things: the first lets the
// page say no profile is usable, the second must not mislabel every stored pair.
const profilesLoaded = ref(false);
const profilesError = ref(false);
const dataQualityLoaded = ref(false);

interface DatabaseGroup {
  instance: string;
  title: string;
  databases: Array<{ name: string }>;
}
const databaseGroups = ref<DatabaseGroup[]>([]);
const databasesLoading = ref(true);

const capabilities = [
  {
    id: "suggest",
    title: t("qualityAgents.capabilitySuggest"),
    description: t("qualityAgents.capabilitySuggestHint"),
  },
  {
    id: "adopt",
    title: t("qualityAgents.capabilityAdopt"),
    description: t("qualityAgents.capabilityAdoptHint"),
  },
  {
    id: "patrol",
    title: t("qualityAgents.capabilityPatrol"),
    description: t("qualityAgents.capabilityPatrolHint"),
  },
];

const weekdays = [
  { value: "1", key: "dataQualitySettings.weekdayMonday" },
  { value: "2", key: "dataQualitySettings.weekdayTuesday" },
  { value: "3", key: "dataQualitySettings.weekdayWednesday" },
  { value: "4", key: "dataQualitySettings.weekdayThursday" },
  { value: "5", key: "dataQualitySettings.weekdayFriday" },
  { value: "6", key: "dataQualitySettings.weekdaySaturday" },
  { value: "7", key: "dataQualitySettings.weekdaySunday" },
];

const lowRiskRuleTypes = [
  QualityRuleType.ROW_COUNT,
  QualityRuleType.NULL_PERCENT,
  QualityRuleType.UNIQUE,
  QualityRuleType.ACCEPTED_VALUES,
  QualityRuleType.RANGE,
  QualityRuleType.STRING_LENGTH,
  QualityRuleType.FRESHNESS,
];

const createOpen = ref(false);
const editingAgent = ref<QualityAgent>();
const creating = ref(false);
const createError = ref("");
const overlapAcknowledged = ref(false);
const form = ref({
  displayName: "",
  description: "",
  owner: "",
  language: "en-US",
  providerProfile: "",
  providerModel: "",
  capabilities: [] as string[],
  databases: [] as string[],
  start: true,
  reportFrequency: "daily",
  reportTime: "09:00",
  reportTimezone: "UTC",
  reportWeekday: "1",
  adoptionMode: "paused",
  adoptionMaxPerDay: 10,
  adoptionConfidence: 0.7,
  adoptionAllowCritical: false,
  adoptionRuleTypes: [] as number[],
  budgetTokensPerHour: 0,
  budgetWakesPerHour: 0,
  budgetMaxConcurrentSessions: 0,
  budgetMaxNewSessionsPerHour: 0,
  budgetMaxActiveSessions: 0,
  budgetConversationMessagesPerHour: 60,
});

const modelOptions = computed(() =>
  enabledModelsFor(providerProfiles.value, form.value.providerProfile)
);

// A stored pair that no longer resolves stays selectable and marked, so an edit cannot
// silently move the agent to another model.
const providerUnavailable = computed(
  () =>
    profilesLoaded.value &&
    form.value.providerProfile !== "" &&
    !providerOptions.value.some(
      (option) => option.name === form.value.providerProfile
    )
);
const unavailableProviderLabel = computed(
  () =>
    `${profileTitle(form.value.providerProfile, providerProfiles.value)} (${t("qualityAgents.providerUnavailable")})`
);
const modelUnavailable = computed(
  () =>
    profilesLoaded.value &&
    form.value.providerModel !== "" &&
    !modelOptions.value.includes(form.value.providerModel)
);

const canCreate = computed(
  () =>
    form.value.displayName.trim() !== "" &&
    form.value.owner !== "" &&
    form.value.providerProfile !== "" &&
    form.value.providerModel !== "" &&
    form.value.capabilities.length > 0 &&
    form.value.databases.length > 0 &&
    (!hasOverlap.value || overlapAcknowledged.value)
);

const hasOverlap = computed(() =>
  form.value.databases.some((name) => overlap(name).length > 0)
);

function overlap(database: string): string[] {
  return agents.value
    .filter((agent) => agent.name !== editingAgent.value?.name)
    .filter((agent) => agent.databases.includes(database))
    .map((agent) => agent.displayName);
}

function databaseName(name: string): string {
  return name.split("/").pop() ?? name;
}

function ownerLabel(owner: string): string {
  const user = users.value.find((candidate) => candidate.name === owner);
  return user?.email ?? owner;
}

function capabilityLabel(id: string): string {
  const capability = capabilities.find((candidate) => candidate.id === id);
  return capability?.title ?? id;
}

function stateLabel(state: QualityAgentState): string {
  switch (state) {
    case QualityAgentState.ACTIVE:
      return t("qualityAgents.stateActive");
    case QualityAgentState.PAUSED:
      return t("qualityAgents.statePaused");
    case QualityAgentState.DISABLED:
      return t("qualityAgents.stateDisabled");
    case QualityAgentState.DRAFT:
      return t("qualityAgents.stateDraft");
    default:
      return t("qualityAgents.stateUnspecified");
  }
}

function stateVariant(state: QualityAgentState) {
  switch (state) {
    case QualityAgentState.ACTIVE:
      return "success" as const;
    case QualityAgentState.PAUSED:
      return "warning" as const;
    case QualityAgentState.DISABLED:
      return "neutral" as const;
    default:
      return "outline" as const;
  }
}

function agentHomeRoute(name: string) {
  return qualityAgentRoute(name);
}

function agentProviderLabel(agent: QualityAgent): string {
  if (!agent.providerProfile || !agent.providerModel) {
    return t("qualityAgents.providerNotConfigured");
  }
  const label = `${profileTitle(agent.providerProfile, providerProfiles.value)} · ${agent.providerModel}`;
  if (!profilesLoaded.value) {
    return label;
  }
  const models = enabledModelsFor(
    providerProfiles.value,
    agent.providerProfile
  );
  if (
    !providerOptions.value.some(
      (option) => option.name === agent.providerProfile
    ) ||
    !models.includes(agent.providerModel)
  ) {
    return `${label} (${t("qualityAgents.providerUnavailable")})`;
  }
  return label;
}

function setProviderProfile(value: unknown) {
  const next = String(value);
  // The select emits the same value when the selected item is clicked again; re-running
  // the model adjustment would silently replace a pinned model.
  if (next === form.value.providerProfile) {
    return;
  }
  form.value.providerProfile = next;
  const models = enabledModelsFor(providerProfiles.value, next);
  if (models.includes(form.value.providerModel)) {
    return;
  }
  // One model is unambiguous; several require the administrator to name one, because
  // models inside a profile differ in price and quality.
  form.value.providerModel = models.length === 1 ? models[0] : "";
}

function toggleAdoptionRuleType(ruleType: number, checked: boolean) {
  form.value.adoptionRuleTypes = checked
    ? [...form.value.adoptionRuleTypes, ruleType]
    : form.value.adoptionRuleTypes.filter(
        (candidate) => candidate !== ruleType
      );
}

function toggleCapability(id: string, checked: boolean) {
  if (checked) {
    form.value.capabilities = form.value.capabilities.includes(id)
      ? form.value.capabilities
      : [...form.value.capabilities, id];
    // Adopt is meaningless without Suggest; adding it also adds the prerequisite.
    if (id === "adopt" && !form.value.capabilities.includes("suggest")) {
      form.value.capabilities = [...form.value.capabilities, "suggest"];
    }
    return;
  }
  form.value.capabilities = form.value.capabilities.filter(
    (candidate) => candidate !== id
  );
  if (id === "suggest") {
    form.value.capabilities = form.value.capabilities.filter(
      (candidate) => candidate !== "adopt"
    );
  }
}

function toggleDatabase(name: string, checked: boolean) {
  form.value.databases = checked
    ? [...form.value.databases, name]
    : form.value.databases.filter((candidate) => candidate !== name);
  if (!hasOverlap.value) {
    overlapAcknowledged.value = false;
  }
}

async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [agentList, setting, userList] = await Promise.all([
      listQualityAgents(),
      getWorkspaceProfileSetting(),
      listAllUsers(),
    ]);
    agents.value = agentList;
    users.value = userList;
    dataQuality.value =
      setting.dataQuality ?? create(DataQualitySettingSchema, {});
    dataQualityLoaded.value = true;
    switches.value = {
      runnerEnabled: dataQuality.value.agentRunnerEnabled,
    };
    await loadProfiles(setting.allowedLlmProviderProfiles ?? []);
  } catch (err) {
    error.value = formatError(err);
  } finally {
    loading.value = false;
  }
}

/**
 * Loads the profiles the dialog may offer. A denied or failed read is remembered, so the
 * page reports "could not load" instead of claiming every stored pair is unavailable.
 */
async function loadProfiles(allowed: string[]) {
  try {
    const profiles = await listAll(async (pageToken) => {
      const response = await listProfiles({ pageSize: 100, pageToken });
      return {
        items: response.profiles,
        nextPageToken: response.nextPageToken,
      };
    });
    providerProfiles.value = profiles;
    providerOptions.value = buildProviderOptions(profiles, allowed);
    profilesLoaded.value = true;
    profilesError.value = false;
  } catch {
    providerProfiles.value = [];
    providerOptions.value = [];
    profilesLoaded.value = false;
    profilesError.value = true;
  }
}

async function loadDatabases() {
  databasesLoading.value = true;
  try {
    const databases = await listAll(async (pageToken) => {
      const response = await listDatabases({
        parent: "workspaces/-",
        pageSize: 1000,
        pageToken,
      });
      return {
        items: response.databases,
        nextPageToken: response.nextPageToken,
      };
    });
    const groups = new Map<string, DatabaseGroup>();
    for (const database of databases) {
      const instance = database.name.split("/").slice(0, 2).join("/");
      let group = groups.get(instance);
      if (!group) {
        group = {
          instance,
          title: instance.split("/").pop() ?? instance,
          databases: [],
        };
        groups.set(instance, group);
      }
      group.databases.push({ name: database.name });
    }
    databaseGroups.value = [...groups.values()];
  } finally {
    databasesLoading.value = false;
  }
}

async function saveSwitch(kind: "runner", value: boolean) {
  savingSwitch.value = true;
  try {
    const next = create(DataQualitySettingSchema, { ...dataQuality.value });
    if (kind === "runner") {
      next.agentRunnerEnabled = value;
    }
    const updated = await updateDataQualitySetting(next);
    dataQuality.value = updated.dataQuality ?? next;
    switches.value = {
      runnerEnabled: dataQuality.value.agentRunnerEnabled,
    };
    notify.success(t("qualityAgents.saved"));
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    savingSwitch.value = false;
  }
}

function openCreate() {
  editingAgent.value = undefined;
  form.value = {
    displayName: "",
    description: "",
    owner: users.value[0]?.name ?? "",
    language: "follow",
    providerProfile: "",
    providerModel: "",
    capabilities: ["suggest", "patrol"],
    databases: [],
    start: true,
    reportFrequency: "daily",
    reportTime: "09:00",
    reportTimezone: "UTC",
    reportWeekday: "1",
    adoptionMode: "paused",
    adoptionMaxPerDay: 10,
    adoptionConfidence: 0.7,
    adoptionAllowCritical: false,
    adoptionRuleTypes: [],
    budgetTokensPerHour: 0,
    budgetWakesPerHour: 0,
    budgetMaxConcurrentSessions: 0,
    budgetMaxNewSessionsPerHour: 0,
    budgetMaxActiveSessions: 0,
    budgetConversationMessagesPerHour: 60,
  };
  overlapAcknowledged.value = false;
  createError.value = "";
  createOpen.value = true;
}

function openEdit(agent: QualityAgent) {
  editingAgent.value = agent;
  form.value = {
    displayName: agent.displayName,
    description: agent.description,
    owner: agent.owner,
    language: agent.language || "follow",
    providerProfile: agent.providerProfile,
    providerModel: agent.providerModel,
    capabilities: [...agent.capabilities],
    databases: [...agent.databases],
    start: agent.state === QualityAgentState.ACTIVE,
    reportFrequency: reportFrequencyValue(agent.reportSchedule?.frequency),
    reportTime: agent.reportSchedule?.time || "09:00",
    reportTimezone: agent.reportSchedule?.timezone || "UTC",
    reportWeekday: String(agent.reportSchedule?.dayOfWeek || 1),
    adoptionMode: adoptionModeValue(agent.adoption?.mode),
    adoptionMaxPerDay: agent.adoption?.maxPerDay || 10,
    adoptionConfidence: agent.adoption?.confidenceThreshold || 0.7,
    adoptionAllowCritical: agent.adoption?.allowCritical ?? false,
    adoptionRuleTypes: [...(agent.adoption?.ruleTypes ?? [])],
    budgetTokensPerHour: Number(agent.budget?.tokensPerHour ?? 0n),
    budgetWakesPerHour: agent.budget?.wakesPerHour ?? 0,
    budgetMaxConcurrentSessions: agent.budget?.maxConcurrentSessions ?? 0,
    budgetMaxNewSessionsPerHour: agent.budget?.maxNewSessionsPerHour ?? 0,
    budgetMaxActiveSessions: agent.budget?.maxActiveSessions ?? 0,
    budgetConversationMessagesPerHour:
      agent.budget?.conversationMessagesPerHour ?? 60,
  };
  overlapAcknowledged.value = false;
  createError.value = "";
  createOpen.value = true;
}

function reportFrequencyValue(
  value: QualityAgentReportFrequency | undefined
): string {
  switch (value) {
    case QualityAgentReportFrequency.WEEKLY:
      return "weekly";
    case QualityAgentReportFrequency.DAILY:
      return "daily";
    default:
      return "off";
  }
}

function adoptionModeValue(
  value: QualityAgentAdoptionMode | undefined
): string {
  switch (value) {
    case QualityAgentAdoptionMode.ACTIVE:
      return "active";
    case QualityAgentAdoptionMode.PAUSED:
      return "paused";
    default:
      return "paused";
  }
}

async function submitEdit() {
  if (!editingAgent.value) {
    return;
  }
  creating.value = true;
  createError.value = "";
  try {
    const next = create(QualityAgentSchema, {
      name: editingAgent.value.name,
      displayName: form.value.displayName.trim(),
      description: form.value.description.trim(),
      owner: form.value.owner,
      language: form.value.language === "follow" ? "" : form.value.language,
      providerProfile: form.value.providerProfile,
      providerModel: form.value.providerModel,
      databases: [...form.value.databases],
      capabilities: [...form.value.capabilities],
    });
    next.reportSchedule = create(QualityAgentReportScheduleSchema, {
      frequency: form.value.capabilities.includes("patrol")
        ? reportFrequencyEnum(form.value.reportFrequency)
        : QualityAgentReportFrequency.OFF,
      time: form.value.reportTime,
      timezone: form.value.reportTimezone,
      dayOfWeek: Number(form.value.reportWeekday),
    });
    next.adoption = create(QualityAgentAdoptionSettingSchema, {
      mode: form.value.capabilities.includes("adopt")
        ? form.value.adoptionMode === "active"
          ? QualityAgentAdoptionMode.ACTIVE
          : QualityAgentAdoptionMode.PAUSED
        : QualityAgentAdoptionMode.OFF,
      ruleTypes: form.value.adoptionRuleTypes,
      confidenceThreshold: form.value.adoptionConfidence,
      maxPerDay: form.value.adoptionMaxPerDay,
      allowCritical: form.value.adoptionAllowCritical,
    });
    next.budget = create(QualityAgentBudgetSchema, {
      tokensPerHour: BigInt(
        Math.max(0, Number(form.value.budgetTokensPerHour) || 0)
      ),
      wakesPerHour: Number(form.value.budgetWakesPerHour) || 0,
      maxConcurrentSessions:
        Number(form.value.budgetMaxConcurrentSessions) || 0,
      maxNewSessionsPerHour:
        Number(form.value.budgetMaxNewSessionsPerHour) || 0,
      maxActiveSessions: Number(form.value.budgetMaxActiveSessions) || 0,
      conversationMessagesPerHour:
        Number(form.value.budgetConversationMessagesPerHour) || 0,
    });
    const paths = [
      "display_name",
      "description",
      "owner",
      "language",
      "databases",
      "capabilities",
      "report_schedule",
      "adoption",
      "budget",
    ];
    // Only write the pair when it actually changed. Resending the value this dialog loaded
    // would revert a provider change another administrator made meanwhile.
    if (
      form.value.providerProfile !== editingAgent.value.providerProfile ||
      form.value.providerModel !== editingAgent.value.providerModel
    ) {
      paths.push("provider_profile", "provider_model");
    }
    await updateQualityAgent(next, paths);
    createOpen.value = false;
    editingAgent.value = undefined;
    await load();
    notify.success(t("qualityAgents.updated"));
  } catch (err) {
    createError.value = formatError(err);
  } finally {
    creating.value = false;
  }
}

function reportFrequencyEnum(value: string): QualityAgentReportFrequency {
  switch (value) {
    case "weekly":
      return QualityAgentReportFrequency.WEEKLY;
    case "daily":
      return QualityAgentReportFrequency.DAILY;
    default:
      return QualityAgentReportFrequency.OFF;
  }
}

async function submitCreate() {
  if (editingAgent.value) {
    await submitEdit();
    return;
  }
  creating.value = true;
  createError.value = "";
  try {
    const config: QualityAgentConfigInput = {
      displayName: form.value.displayName.trim(),
      description: form.value.description.trim(),
      owner: form.value.owner,
      language: form.value.language === "follow" ? "" : form.value.language,
      providerProfile: form.value.providerProfile,
      providerModel: form.value.providerModel,
      databases: form.value.databases,
      capabilities: form.value.capabilities,
      reportSchedule: form.value.capabilities.includes("patrol")
        ? {
            frequency:
              form.value.reportFrequency === "weekly"
                ? QualityAgentReportFrequency.WEEKLY
                : form.value.reportFrequency === "daily"
                  ? QualityAgentReportFrequency.DAILY
                  : QualityAgentReportFrequency.OFF,
            time: form.value.reportTime,
            timezone: form.value.reportTimezone,
            dayOfWeek: Number(form.value.reportWeekday),
          }
        : { frequency: QualityAgentReportFrequency.OFF },
      adoption: form.value.capabilities.includes("adopt")
        ? {
            mode:
              form.value.adoptionMode === "active"
                ? QualityAgentAdoptionMode.ACTIVE
                : QualityAgentAdoptionMode.PAUSED,
            ruleTypes: form.value.adoptionRuleTypes,
            confidenceThreshold: form.value.adoptionConfidence,
            maxPerDay: form.value.adoptionMaxPerDay,
            allowCritical: form.value.adoptionAllowCritical,
          }
        : { mode: QualityAgentAdoptionMode.OFF },
      budget: {
        tokensPerHour: BigInt(
          Math.max(0, Number(form.value.budgetTokensPerHour) || 0)
        ),
        wakesPerHour: Number(form.value.budgetWakesPerHour) || 0,
        maxConcurrentSessions:
          Number(form.value.budgetMaxConcurrentSessions) || 0,
        maxNewSessionsPerHour:
          Number(form.value.budgetMaxNewSessionsPerHour) || 0,
        maxActiveSessions: Number(form.value.budgetMaxActiveSessions) || 0,
        conversationMessagesPerHour:
          Number(form.value.budgetConversationMessagesPerHour) || 0,
      },
    };
    await createQualityAgent({
      config,
      start: form.value.start,
    });
    createOpen.value = false;
    await load();
    notify.success(t("qualityAgents.created"));
  } catch (err) {
    createError.value = formatError(err);
  } finally {
    creating.value = false;
  }
}

async function pause(agent: QualityAgent) {
  busyAgent.value = agent.name;
  try {
    await pauseQualityAgent(agent.name);
    await load();
    notify.success(t("qualityAgents.paused"));
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busyAgent.value = "";
  }
}

async function start(agent: QualityAgent) {
  busyAgent.value = agent.name;
  try {
    await startQualityAgent(agent.name);
    await load();
    notify.success(t("qualityAgents.started"));
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busyAgent.value = "";
  }
}

async function disable(agent: QualityAgent) {
  busyAgent.value = agent.name;
  try {
    await disableQualityAgent(agent.name);
    await load();
    notify.success(t("qualityAgents.disabledNotice"));
  } catch (err) {
    notify.error(formatError(err));
  } finally {
    busyAgent.value = "";
  }
}

onMounted(async () => {
  await Promise.all([load(), loadDatabases()]);
});
</script>
