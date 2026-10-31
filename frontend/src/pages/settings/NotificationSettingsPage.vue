<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('notificationSettings.title')"
      :description="t('notificationSettings.pageDescription')"
    />

    <!-- Channels: who an outbound message goes to and for what. -->
    <Card>
      <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
        <div class="space-y-1">
          <CardTitle class="text-base">
            {{ t("notificationSettings.channels") }}
          </CardTitle>
          <CardDescription>
            {{ t("notificationSettings.channelsDescription") }}
          </CardDescription>
        </div>
        <Button
          size="sm"
          :disabled="!canUpdate"
          @click="openCreate"
        >
          <Plus class="mr-1.5 h-4 w-4" />
          {{ t("notificationSettings.newChannel") }}
        </Button>
      </CardHeader>
      <CardContent class="pt-4">
        <PageState :loading="loading">
          <div
            v-if="channels.length === 0"
            class="px-4 py-10 text-center"
          >
            <p class="text-sm font-medium">
              {{ t("notificationSettings.emptyTitle") }}
            </p>
            <p class="mt-1 text-sm text-muted-foreground">
              {{ t("notificationSettings.emptyDescription") }}
            </p>
          </div>
          <div
            v-else
            class="rounded-md border"
          >
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("notificationSettings.name") }}</TableHead>
                <TableHead>{{ t("notificationSettings.kind") }}</TableHead>
                <TableHead>{{ t("notificationSettings.subscription") }}</TableHead>
                <TableHead>{{ t("notificationSettings.status") }}</TableHead>
                <TableHead class="text-right">
                  {{ t("common.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="channel in channels"
                :key="channel.name"
              >
                <TableCell class="font-medium">
                  {{ channel.displayName }}
                </TableCell>
                <TableCell>
                  {{ channelKindLabel(channel.kind) }}
                </TableCell>
                <TableCell class="text-xs text-muted-foreground">
                  {{ subscriptionLabel(channel) }}
                </TableCell>
                <TableCell>
                  <Badge
                    :variant="channel.enabled ? 'default' : 'secondary'"
                    class="font-normal"
                  >
                    {{
                      channel.enabled
                        ? t("notificationSettings.enabled")
                        : t("notificationSettings.disabled")
                    }}
                  </Badge>
                </TableCell>
                <TableCell class="text-right">
                  <div class="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      :disabled="!canUpdate || testing === channel.name"
                      @click="testChannel(channel)"
                    >
                      <Loader2
                        v-if="testing === channel.name"
                        class="mr-1 h-4 w-4 animate-spin"
                      />
                      {{ t("notificationSettings.test") }}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      :disabled="!canUpdate"
                      @click="openEdit(channel)"
                    >
                      {{ t("common.edit") }}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      class="text-destructive"
                      :disabled="!canUpdate"
                      @click="confirmDelete(channel)"
                    >
                      {{ t("common.delete") }}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          </div>
        </PageState>
      </CardContent>
    </Card>

    <!-- Email delivery: the SMTP server an EMAIL channel sends through. -->
    <Card>
      <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
        <div class="space-y-1">
          <CardTitle class="text-base">
            {{ t("notificationSettings.emailDelivery") }}
          </CardTitle>
          <CardDescription>
            {{ t("notificationSettings.emailDeliveryDescription") }}
          </CardDescription>
        </div>
        <div class="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            :disabled="!canUpdate || emailLoading || emailSaving || emailTesting"
            @click="onSendTestEmail"
          >
            <Loader2
              v-if="emailTesting"
              class="mr-1.5 h-4 w-4 animate-spin"
            />
            {{ t("notificationSettings.sendTestEmail") }}
          </Button>
          <Button
            size="sm"
            :disabled="!canUpdate || emailLoading || emailSaving || emailTesting"
            @click="saveEmailSetting"
          >
            <Loader2
              v-if="emailSaving"
              class="mr-1.5 h-4 w-4 animate-spin"
            />
            {{ t("common.save") }}
          </Button>
        </div>
      </CardHeader>
      <CardContent class="pt-4">
        <p
          v-if="!canGetEmailSetting"
          class="px-4 py-6 text-center text-sm text-muted-foreground"
        >
          {{ t("notificationSettings.emailReadOnly") }}
        </p>
        <PageState
          v-else
          :loading="emailLoading"
        >
          <div class="grid gap-4 sm:grid-cols-2">
            <FormField
              v-model="emailForm.from"
              :label="t('notificationSettings.emailFrom')"
              placeholder="notifications@example.com"
            />
            <FormField
              v-model="emailForm.fromName"
              :label="t('notificationSettings.emailFromName')"
            />
            <FormField
              v-model="emailForm.host"
              :label="t('notificationSettings.smtpHost')"
              placeholder="smtp.example.com"
            />
            <FormField
              v-model="emailForm.port"
              type="number"
              :label="t('notificationSettings.smtpPort')"
            />
            <div class="space-y-2.5">
              <Label>{{ t("notificationSettings.smtpEncryption") }}</Label>
              <Select
                :model-value="String(emailForm.encryption)"
                @update:model-value="onEncryptionChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">
                    {{ t("notificationSettings.encryptionNone") }}
                  </SelectItem>
                  <SelectItem value="2">STARTTLS</SelectItem>
                  <SelectItem value="3">SSL/TLS</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="space-y-2.5">
              <Label>{{ t("notificationSettings.smtpAuthentication") }}</Label>
              <Select
                :model-value="String(emailForm.authentication)"
                @update:model-value="onAuthenticationChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">
                    {{ t("notificationSettings.authenticationNone") }}
                  </SelectItem>
                  <SelectItem value="2">PLAIN</SelectItem>
                  <SelectItem value="3">LOGIN</SelectItem>
                  <SelectItem value="4">CRAM-MD5</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <FormField
              v-model="emailForm.username"
              :label="t('notificationSettings.smtpUsername')"
            />
            <FormField
              v-model="emailForm.password"
              type="password"
              :label="t('notificationSettings.smtpPassword')"
              :hint="
                emailPasswordSet
                  ? t('notificationSettings.smtpPasswordKeepHint')
                  : ''
              "
            />
          </div>
        </PageState>
      </CardContent>
    </Card>

    <!-- Deliveries: what was queued, what was sent, what is stuck. -->
    <Card>
      <CardHeader class="flex flex-row flex-wrap items-start justify-between gap-3 border-b">
        <div class="space-y-1">
          <CardTitle class="text-base">
            {{ t("notificationSettings.deliveries") }}
          </CardTitle>
          <CardDescription>
            {{ t("notificationSettings.deliveriesDescription") }}
          </CardDescription>
        </div>
        <div class="flex items-center gap-2">
          <Select
            :model-value="deliveryFilter"
            @update:model-value="onDeliveryFilter"
          >
            <SelectTrigger class="h-8 w-44">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">
                {{ t("notificationSettings.allStates") }}
              </SelectItem>
              <SelectItem value="dead">
                {{ t("notificationSettings.stateDead") }}
              </SelectItem>
              <SelectItem value="pending">
                {{ t("notificationSettings.statePending") }}
              </SelectItem>
              <SelectItem value="sending">
                {{ t("notificationSettings.stateSending") }}
              </SelectItem>
              <SelectItem value="sent">
                {{ t("notificationSettings.stateSent") }}
              </SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            size="sm"
            @click="refreshDeliveries"
          >
            <RefreshCcw class="mr-1.5 h-4 w-4" />
            {{ t("common.refresh") }}
          </Button>
        </div>
      </CardHeader>
      <CardContent class="pt-4">
        <PageState :loading="deliveriesLoading">
          <div
            v-if="deliveries.length === 0"
            class="px-4 py-10 text-center"
          >
            <p class="text-sm font-medium">
              {{ t("notificationSettings.noDeliveriesTitle") }}
            </p>
            <p class="mt-1 text-sm text-muted-foreground">
              {{ t("notificationSettings.noDeliveriesDescription") }}
            </p>
          </div>
          <div
            v-else
            class="rounded-md border"
          >
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("notificationSettings.deliveryTime") }}</TableHead>
                <TableHead>{{ t("notificationSettings.channel") }}</TableHead>
                <TableHead>{{ t("notificationSettings.event") }}</TableHead>
                <TableHead>{{ t("notificationSettings.status") }}</TableHead>
                <TableHead>{{ t("notificationSettings.attempts") }}</TableHead>
                <TableHead>{{ t("notificationSettings.nextAttempt") }}</TableHead>
                <TableHead>{{ t("notificationSettings.lastError") }}</TableHead>
                <TableHead class="text-right">
                  {{ t("common.actions") }}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="delivery in deliveries"
                :key="delivery.name"
              >
                <TableCell class="whitespace-nowrap text-xs text-muted-foreground">
                  {{ formatDateTime(delivery.createTime, locale) }}
                </TableCell>
                <TableCell class="text-xs">
                  {{ delivery.channelName || "—" }}
                </TableCell>
                <TableCell class="whitespace-nowrap text-xs">
                  {{ eventTypeLabel(delivery.eventType) }}
                </TableCell>
                <TableCell>
                  <Badge
                    :variant="deliveryStateVariant(delivery.state)"
                    class="font-normal"
                  >
                    {{ deliveryStateLabel(delivery.state) }}
                  </Badge>
                </TableCell>
                <TableCell class="text-xs">
                  {{ delivery.attempts }}
                </TableCell>
                <TableCell class="whitespace-nowrap text-xs text-muted-foreground">
                  {{
                    delivery.nextAttemptTime
                      ? formatDateTime(delivery.nextAttemptTime, locale)
                      : "—"
                  }}
                </TableCell>
                <TableCell
                  class="max-w-[22rem] truncate text-xs text-muted-foreground"
                  :title="delivery.lastError"
                >
                  {{ delivery.lastError || "—" }}
                </TableCell>
                <TableCell class="text-right">
                  <Button
                    v-if="delivery.state === NotificationDeliveryState.DEAD"
                    variant="ghost"
                    size="sm"
                    :disabled="!canUpdate"
                    @click="retryDelivery(delivery)"
                  >
                    {{ t("notificationSettings.retry") }}
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
          </div>
        </PageState>
        <div
          v-if="nextDeliveryToken"
          class="mt-3 flex justify-center"
        >
          <Button
            variant="outline"
            size="sm"
            :disabled="deliveriesLoading"
            @click="loadMoreDeliveries"
          >
            {{ t("notificationSettings.loadMore") }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <!-- Create / edit one channel. -->
    <Dialog
      :open="dialogOpen"
      @update:open="(open) => (dialogOpen = open)"
    >
      <DialogContent class="max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {{
              editingChannel
                ? t("notificationSettings.editChannel")
                : t("notificationSettings.newChannel")
            }}
          </DialogTitle>
          <DialogDescription>
            {{ t("notificationSettings.dialogDescription") }}
          </DialogDescription>
        </DialogHeader>

        <div class="space-y-4">
          <FormField
            v-model="form.displayName"
            :label="t('notificationSettings.name')"
            :placeholder="t('notificationSettings.namePlaceholder')"
            required
          />

          <div class="space-y-2.5">
            <Label>{{ t("notificationSettings.kind") }}</Label>
            <Select
              v-if="!editingChannel"
              :model-value="String(form.kind)"
              @update:model-value="onKindChange"
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  v-for="option in kindOptions"
                  :key="option.value"
                  :value="String(option.value)"
                >
                  {{ option.label }}
                </SelectItem>
              </SelectContent>
            </Select>
            <p
              v-else
              class="text-sm"
            >
              {{ channelKindLabel(form.kind) }}
            </p>
          </div>

          <template v-if="isEmailKind">
            <FormField
              v-model="form.addresses"
              :label="t('notificationSettings.recipients')"
              :placeholder="t('notificationSettings.recipientsPlaceholder')"
              :hint="t('notificationSettings.recipientsHint')"
            />
            <div class="flex items-start gap-2">
              <Checkbox
                :model-value="form.includeAudience"
                @update:model-value="onIncludeAudienceChange"
              />
              <Label class="text-sm leading-5">
                {{ t("notificationSettings.includeAudience") }}
              </Label>
            </div>
            <p class="text-xs text-muted-foreground">
              {{ t("notificationSettings.emailSmtpHint") }}
            </p>
          </template>
          <template v-else>
            <FormField
              v-model="form.endpoint"
              :label="t('notificationSettings.endpoint')"
              placeholder="https://..."
              :hint="endpointHint"
              :required="!editingChannel"
            />
            <template v-if="supportsSigning">
              <div class="flex items-center gap-2">
                <Checkbox
                  :model-value="form.sign"
                  @update:model-value="onSignChange"
                />
                <Label class="text-sm">
                  {{ signingLabel }}
                </Label>
              </div>
              <FormField
                v-if="form.sign"
                v-model="form.signingSecret"
                type="password"
                :label="t('notificationSettings.signingSecret')"
                :hint="signingSecretHint"
              />
            </template>
          </template>

          <div class="grid gap-4 sm:grid-cols-2">
            <div class="space-y-2.5">
              <Label>{{ t("notificationSettings.minSeverity") }}</Label>
              <Select
                :model-value="String(form.minSeverity)"
                @update:model-value="onMinSeverityChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">
                    {{ t("notificationSettings.severityAny") }}
                  </SelectItem>
                  <SelectItem value="1">
                    {{ t("notificationSettings.severityInfo") }}
                  </SelectItem>
                  <SelectItem value="2">
                    {{ t("notificationSettings.severityWarning") }}
                  </SelectItem>
                  <SelectItem value="3">
                    {{ t("notificationSettings.severityError") }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="space-y-2.5">
              <Label>{{ t("notificationSettings.language") }}</Label>
              <Select
                :model-value="form.language"
                @update:model-value="onLanguageChange"
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="zh-CN">简体中文</SelectItem>
                  <SelectItem value="en-US">English</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div class="space-y-2.5">
            <Label>{{ t("notificationSettings.eventTypes") }}</Label>
            <div class="space-y-2">
              <label
                v-for="option in eventTypeOptions"
                :key="option.value"
                class="flex items-center gap-2 text-sm"
              >
                <Checkbox
                  :model-value="form.eventTypes.includes(option.value)"
                  @update:model-value="(value: boolean | 'indeterminate') => toggleEventType(option.value, value === true)"
                />
                <span>{{ option.label }}</span>
              </label>
              <p class="text-xs text-muted-foreground">
                {{ t("notificationSettings.eventTypesHint") }}
              </p>
            </div>
          </div>

          <div class="flex items-center gap-2">
            <Checkbox
              :model-value="form.enabled"
              @update:model-value="onEnabledChange"
            />
            <Label class="text-sm">{{ t("notificationSettings.enabled") }}</Label>
          </div>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            @click="dialogOpen = false"
          >
            {{ t("common.cancel") }}
          </Button>
          <Button
            :disabled="saving"
            @click="saveChannel"
          >
            <Loader2
              v-if="saving"
              class="mr-1.5 h-4 w-4 animate-spin"
            />
            {{ t("common.save") }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDeleteDialog
      v-model="deleteOpen"
      :title="t('notificationSettings.deleteTitle')"
      :message="t('notificationSettings.deleteDescription')"
      :item-name="pendingDelete?.displayName ?? ''"
      @confirm="deleteChannel"
    />
  </div>
</template>

<script setup lang="ts">
import { create } from "@bufbuild/protobuf";
import { Loader2, Plus, RefreshCcw } from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  createNotificationChannel,
  deleteNotificationChannel,
  listNotificationChannels,
  listNotificationDeliveriesPage,
  retryNotificationDelivery,
  testNotificationChannel,
  updateNotificationChannel,
} from "@/api/notification";
import {
  getEmailSetting,
  sendTestEmail,
  updateEmailSetting,
} from "@/api/setting";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import PageState from "@/components/common/PageState.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
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
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import FormField from "@/components/ui/form-field/FormField.vue";
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
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import {
  type NotificationChannel,
  NotificationChannelConfigSchema,
  NotificationChannelKind,
  NotificationChannelSchema,
  NotificationChannelSecretSchema,
  type NotificationDelivery,
  NotificationDeliveryState,
  NotificationSeverity,
  NotificationType,
} from "@/types/proto-es/v1/notification_service_pb";
import {
  EmailSettingSchema,
  SMTPAuthentication,
  SMTPConfigSchema,
  SMTPEncryption,
} from "@/types/proto-es/v1/setting_service_pb";
import { formatDateTime } from "@/utils/datetime";

/**
 * Outbound notification channels. A channel is a destination an administrator
 * configures; every in-app message an event produces is posted to it once. The
 * credential (the endpoint URL, and the signing key when signing) is write-only:
 * the read reports only whether one is stored, so editing keeps it unless the
 * field is filled in again. An email channel has no credential — it delivers
 * through the workspace SMTP setting rendered above the delivery table.
 */

const { t, locale } = useI18n();
const authStore = useAuthStore();
const { formatError } = useErrorHandler();

const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
);
const canGetEmailSetting = computed(() =>
  authStore.hasPermission("metaxisdata.settings.get")
);

const loading = ref(false);
const channels = ref<NotificationChannel[]>([]);
const testing = ref("");

const deliveriesLoading = ref(false);
const deliveries = ref<NotificationDelivery[]>([]);
const nextDeliveryToken = ref("");
const deliveryFilter = ref<"all" | "pending" | "sending" | "dead" | "sent">(
  "all"
);

const dialogOpen = ref(false);
const saving = ref(false);
const editingChannel = ref<NotificationChannel | null>(null);
const pendingDelete = ref<NotificationChannel | null>(null);
const deleteOpen = ref(false);

interface ChannelForm {
  displayName: string;
  kind: NotificationChannelKind;
  endpoint: string;
  signingSecret: string;
  sign: boolean;
  addresses: string;
  includeAudience: boolean;
  enabled: boolean;
  eventTypes: NotificationType[];
  minSeverity: NotificationSeverity;
  language: string;
}

const form = reactive<ChannelForm>({
  displayName: "",
  kind: NotificationChannelKind.WEBHOOK,
  endpoint: "",
  signingSecret: "",
  sign: false,
  addresses: "",
  includeAudience: false,
  enabled: true,
  eventTypes: [],
  minSeverity: NotificationSeverity.UNSPECIFIED,
  language: locale.value === "zh-CN" ? "zh-CN" : "en-US",
});

const isEmailKind = computed(() => form.kind === NotificationChannelKind.EMAIL);

const supportsSigning = computed(
  () =>
    form.kind === NotificationChannelKind.WEBHOOK ||
    form.kind === NotificationChannelKind.DINGTALK ||
    form.kind === NotificationChannelKind.FEISHU
);

const kindOptions = computed(() =>
  [
    NotificationChannelKind.WEBHOOK,
    NotificationChannelKind.DINGTALK,
    NotificationChannelKind.WECOM,
    NotificationChannelKind.FEISHU,
    NotificationChannelKind.EMAIL,
  ].map((value) => ({ value, label: channelKindLabel(value) }))
);

const endpointHint = computed(() => {
  if (editingChannel.value) {
    return t("notificationSettings.endpointKeepHint");
  }
  if (form.kind === NotificationChannelKind.WECOM) {
    return t("notificationSettings.wecomEndpointHint");
  }
  return t("notificationSettings.endpointHint");
});

const signingLabel = computed(() =>
  form.kind === NotificationChannelKind.WEBHOOK
    ? t("notificationSettings.signBody")
    : t("notificationSettings.signRequest")
);

const signingSecretHint = computed(() => {
  if (editingChannel.value) {
    return t("notificationSettings.secretKeepHint");
  }
  if (form.kind === NotificationChannelKind.WEBHOOK) {
    return t("notificationSettings.signingSecretHint");
  }
  return t("notificationSettings.robotSigningSecretHint");
});

const eventTypeOptions = computed(() => [
  {
    value: NotificationType.DATA_QUALITY,
    label: t("notificationSettings.eventDataQuality"),
  },
  {
    value: NotificationType.SCHEMA_SYNC,
    label: t("notificationSettings.eventSchemaSync"),
  },
  {
    value: NotificationType.OPENLINEAGE,
    label: t("notificationSettings.eventOpenLineage"),
  },
]);

async function loadChannels() {
  loading.value = true;
  try {
    channels.value = await listNotificationChannels();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    loading.value = false;
  }
}

function deliveryStateForFilter(): NotificationDeliveryState {
  switch (deliveryFilter.value) {
    case "pending":
      return NotificationDeliveryState.PENDING;
    case "dead":
      return NotificationDeliveryState.DEAD;
    case "sending":
      return NotificationDeliveryState.SENDING;
    case "sent":
      return NotificationDeliveryState.SENT;
    default:
      return NotificationDeliveryState.UNSPECIFIED;
  }
}

async function loadDeliveries(reset: boolean) {
  deliveriesLoading.value = true;
  try {
    const page = await listNotificationDeliveriesPage({
      pageSize: 50,
      pageToken: reset ? "" : nextDeliveryToken.value,
      state: deliveryStateForFilter(),
    });
    deliveries.value = reset
      ? page.items
      : [...deliveries.value, ...page.items];
    nextDeliveryToken.value = page.nextPageToken;
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    deliveriesLoading.value = false;
  }
}

async function refreshDeliveries() {
  await loadDeliveries(true);
}

function setDeliveryFilter(value: string) {
  deliveryFilter.value = value as typeof deliveryFilter.value;
  deliveries.value = [];
  nextDeliveryToken.value = "";
  void loadDeliveries(true);
}

async function loadMoreDeliveries() {
  await loadDeliveries(false);
}

// ---- Workspace SMTP setting ------------------------------------------------

interface EmailForm {
  from: string;
  fromName: string;
  host: string;
  port: string;
  encryption: SMTPEncryption;
  authentication: SMTPAuthentication;
  username: string;
  password: string;
}

const emailLoading = ref(false);
const emailSaving = ref(false);
const emailTesting = ref(false);
const emailPasswordSet = ref(false);

// A sensible STARTTLS/587/PLAIN default, so an unset workspace starts from the
// settings most providers document and the select always has a valid option.
const emailForm = reactive<EmailForm>({
  from: "",
  fromName: "",
  host: "",
  port: "587",
  encryption: SMTPEncryption.SMTP_ENCRYPTION_STARTTLS,
  authentication: SMTPAuthentication.SMTP_AUTHENTICATION_PLAIN,
  username: "",
  password: "",
});

async function loadEmailSetting() {
  if (!canGetEmailSetting.value) {
    return;
  }
  emailLoading.value = true;
  try {
    const setting = await getEmailSetting();
    const smtp = setting.smtp;
    emailForm.from = setting.from;
    emailForm.fromName = setting.fromName;
    emailForm.host = smtp?.host ?? "";
    emailForm.port = smtp?.port ? String(smtp.port) : "587";
    // UNSPECIFIED is the zero value of an unset server, so a falsy enum keeps
    // the documented STARTTLS / PLAIN default the select shows.
    emailForm.encryption =
      smtp?.encryption || SMTPEncryption.SMTP_ENCRYPTION_STARTTLS;
    emailForm.authentication =
      smtp?.authentication || SMTPAuthentication.SMTP_AUTHENTICATION_PLAIN;
    emailForm.username = smtp?.username ?? "";
    emailForm.password = "";
    emailPasswordSet.value = setting.passwordSet;
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    emailLoading.value = false;
  }
}

function buildEmailSetting() {
  return create(EmailSettingSchema, {
    from: emailForm.from.trim(),
    fromName: emailForm.fromName.trim(),
    smtp: create(SMTPConfigSchema, {
      host: emailForm.host.trim(),
      port: Number.parseInt(emailForm.port, 10) || 0,
      encryption: emailForm.encryption,
      authentication: emailForm.authentication,
      username: emailForm.username.trim(),
      password: emailForm.password,
    }),
  });
}

async function saveEmailSetting() {
  if (!canUpdate.value) {
    return;
  }
  emailSaving.value = true;
  try {
    await updateEmailSetting(buildEmailSetting());
    notify.success(t("notificationSettings.emailSaved"));
    await loadEmailSetting();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    emailSaving.value = false;
  }
}

async function onSendTestEmail() {
  if (!canUpdate.value) {
    return;
  }
  emailTesting.value = true;
  try {
    await sendTestEmail();
    notify.success(t("notificationSettings.testEmailSent"));
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    emailTesting.value = false;
  }
}

function onEncryptionChange(value: unknown) {
  emailForm.encryption = Number(value) as SMTPEncryption;
}

function onAuthenticationChange(value: unknown) {
  emailForm.authentication = Number(value) as SMTPAuthentication;
}

// ---- Channel dialog --------------------------------------------------------

function resetKindFields() {
  form.endpoint = "";
  form.signingSecret = "";
  form.sign = false;
  form.addresses = "";
  form.includeAudience = false;
}

function resetForm() {
  form.displayName = "";
  form.kind = NotificationChannelKind.WEBHOOK;
  resetKindFields();
  form.enabled = true;
  form.eventTypes = [];
  form.minSeverity = NotificationSeverity.UNSPECIFIED;
  form.language = locale.value === "zh-CN" ? "zh-CN" : "en-US";
}

function openCreate() {
  editingChannel.value = null;
  resetForm();
  dialogOpen.value = true;
}

function openEdit(channel: NotificationChannel) {
  editingChannel.value = channel;
  form.displayName = channel.displayName;
  form.kind = channel.kind;
  resetKindFields();
  const config = channel.config?.config;
  if (
    config?.case === "webhook" ||
    config?.case === "dingtalk" ||
    config?.case === "feishu"
  ) {
    form.sign = Boolean(config.value.sign);
  }
  if (config?.case === "email") {
    form.addresses = config.value.addresses.join(", ");
    form.includeAudience = config.value.includeAudience;
  }
  form.enabled = channel.enabled;
  form.eventTypes = [...channel.eventTypes];
  form.minSeverity = channel.minSeverity;
  form.language = channel.language || "en-US";
  dialogOpen.value = true;
}

function onDeliveryFilter(value: unknown) {
  setDeliveryFilter(String(value));
}

function onKindChange(value: unknown) {
  form.kind = Number(value) as NotificationChannelKind;
  resetKindFields();
}

function onSignChange(value: boolean | "indeterminate") {
  form.sign = value === true;
}

function onIncludeAudienceChange(value: boolean | "indeterminate") {
  form.includeAudience = value === true;
}

function onMinSeverityChange(value: unknown) {
  form.minSeverity = Number(value) as NotificationSeverity;
}

function onLanguageChange(value: unknown) {
  form.language = String(value);
}

function onEnabledChange(value: boolean | "indeterminate") {
  form.enabled = value === true;
}

function toggleEventType(value: NotificationType, checked: boolean) {
  if (checked) {
    if (!form.eventTypes.includes(value)) {
      form.eventTypes.push(value);
    }
    return;
  }
  form.eventTypes = form.eventTypes.filter((item) => item !== value);
}

/** Recipients as typed: commas, semicolons, spaces or newlines separate addresses. */
function parseRecipients(): string[] {
  return form.addresses
    .split(/[\s,;]+/)
    .map((address) => address.trim())
    .filter((address) => address !== "");
}

function buildConfig() {
  switch (form.kind) {
    case NotificationChannelKind.DINGTALK:
      return create(NotificationChannelConfigSchema, {
        config: { case: "dingtalk", value: { sign: form.sign } },
      });
    case NotificationChannelKind.WECOM:
      return create(NotificationChannelConfigSchema, {
        config: { case: "wecom", value: {} },
      });
    case NotificationChannelKind.FEISHU:
      return create(NotificationChannelConfigSchema, {
        config: { case: "feishu", value: { sign: form.sign } },
      });
    case NotificationChannelKind.EMAIL:
      return create(NotificationChannelConfigSchema, {
        config: {
          case: "email",
          value: {
            addresses: parseRecipients(),
            includeAudience: form.includeAudience,
          },
        },
      });
    default:
      return create(NotificationChannelConfigSchema, {
        config: { case: "webhook", value: { sign: form.sign } },
      });
  }
}

function buildChannel(): NotificationChannel {
  return create(NotificationChannelSchema, {
    name: editingChannel.value?.name ?? "",
    displayName: form.displayName.trim(),
    kind: form.kind,
    enabled: form.enabled,
    eventTypes: form.eventTypes,
    minSeverity: form.minSeverity,
    language: form.language,
    config: buildConfig(),
  });
}

function buildSecret() {
  if (isEmailKind.value) {
    return undefined;
  }
  const endpoint = form.endpoint.trim();
  const signingSecret = supportsSigning.value ? form.signingSecret : "";
  if (!endpoint && !signingSecret) {
    return undefined;
  }
  return create(NotificationChannelSecretSchema, {
    endpoint,
    signingSecret,
  });
}

async function saveChannel() {
  if (form.displayName.trim() === "") {
    notify.error(t("notificationSettings.nameRequired"));
    return;
  }
  if (isEmailKind.value) {
    if (parseRecipients().length === 0 && !form.includeAudience) {
      notify.error(t("notificationSettings.recipientsRequired"));
      return;
    }
  } else if (!editingChannel.value && form.endpoint.trim() === "") {
    notify.error(t("notificationSettings.endpointRequired"));
    return;
  }
  if (
    supportsSigning.value &&
    form.sign &&
    !form.signingSecret &&
    !editingChannel.value?.secretSet
  ) {
    notify.error(t("notificationSettings.signingSecretRequired"));
    return;
  }
  saving.value = true;
  try {
    if (editingChannel.value) {
      await updateNotificationChannel(
        buildChannel(),
        [
          "display_name",
          "enabled",
          "event_types",
          "min_severity",
          "language",
          "config",
        ],
        buildSecret()
      );
      notify.success(t("notificationSettings.updated"));
    } else {
      await createNotificationChannel(buildChannel(), buildSecret());
      notify.success(t("notificationSettings.created"));
    }
    dialogOpen.value = false;
    await loadChannels();
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    saving.value = false;
  }
}

async function testChannel(channel: NotificationChannel) {
  testing.value = channel.name;
  try {
    await testNotificationChannel(channel.name);
    notify.success(t("notificationSettings.testSent"));
  } catch (error) {
    notify.error(formatError(error));
  } finally {
    testing.value = "";
  }
}

function confirmDelete(channel: NotificationChannel) {
  pendingDelete.value = channel;
  deleteOpen.value = true;
}

async function deleteChannel() {
  const channel = pendingDelete.value;
  if (!channel) {
    return;
  }
  try {
    await deleteNotificationChannel(channel.name);
    notify.success(t("notificationSettings.deleted"));
    pendingDelete.value = null;
    deleteOpen.value = false;
    await loadChannels();
    await refreshDeliveries();
  } catch (error) {
    notify.error(formatError(error));
  }
}

async function retryDelivery(delivery: NotificationDelivery) {
  try {
    await retryNotificationDelivery(delivery.name);
    notify.success(t("notificationSettings.retried"));
    await refreshDeliveries();
  } catch (error) {
    notify.error(formatError(error));
  }
}

function channelKindLabel(kind: NotificationChannelKind): string {
  switch (kind) {
    case NotificationChannelKind.WEBHOOK:
      return t("notificationSettings.kindWebhook");
    case NotificationChannelKind.DINGTALK:
      return t("notificationSettings.kindDingTalk");
    case NotificationChannelKind.WECOM:
      return t("notificationSettings.kindWeCom");
    case NotificationChannelKind.FEISHU:
      return t("notificationSettings.kindFeishu");
    case NotificationChannelKind.EMAIL:
      return t("notificationSettings.kindEmail");
    default:
      return "—";
  }
}

const listSeparator = computed(() => t("common.listSeparator"));

function subscriptionLabel(channel: NotificationChannel): string {
  const types =
    channel.eventTypes.length === 0
      ? t("notificationSettings.allEvents")
      : channel.eventTypes
          .map((value) => eventTypeLabel(value))
          .join(listSeparator.value);
  return `${types} · ${severityLabel(channel.minSeverity)}`;
}

function eventTypeLabel(value: NotificationType): string {
  switch (value) {
    case NotificationType.DATA_QUALITY:
      return t("notificationSettings.eventDataQuality");
    case NotificationType.SCHEMA_SYNC:
      return t("notificationSettings.eventSchemaSync");
    case NotificationType.OPENLINEAGE:
      return t("notificationSettings.eventOpenLineage");
    default:
      return "—";
  }
}

function severityLabel(value: NotificationSeverity): string {
  switch (value) {
    case NotificationSeverity.INFO:
      return t("notificationSettings.severityInfo");
    case NotificationSeverity.WARNING:
      return t("notificationSettings.severityWarning");
    case NotificationSeverity.ERROR:
      return t("notificationSettings.severityError");
    default:
      return t("notificationSettings.severityAny");
  }
}

function deliveryStateLabel(state: NotificationDeliveryState): string {
  switch (state) {
    case NotificationDeliveryState.PENDING:
      return t("notificationSettings.statePending");
    case NotificationDeliveryState.SENDING:
      return t("notificationSettings.stateSending");
    case NotificationDeliveryState.SENT:
      return t("notificationSettings.stateSent");
    case NotificationDeliveryState.DEAD:
      return t("notificationSettings.stateDead");
    default:
      return "—";
  }
}

function deliveryStateVariant(
  state: NotificationDeliveryState
): "default" | "secondary" | "destructive" | "outline" {
  switch (state) {
    case NotificationDeliveryState.SENT:
      return "default";
    case NotificationDeliveryState.DEAD:
      return "destructive";
    case NotificationDeliveryState.SENDING:
      return "outline";
    default:
      return "secondary";
  }
}

onMounted(async () => {
  await Promise.all([loadChannels(), loadDeliveries(true), loadEmailSetting()]);
});
</script>
