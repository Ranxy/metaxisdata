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

          <FormField
            v-model="form.endpoint"
            :label="t('notificationSettings.endpoint')"
            placeholder="https://..."
            :hint="
              editingChannel
                ? t('notificationSettings.endpointKeepHint')
                : t('notificationSettings.endpointHint')
            "
            :required="!editingChannel"
          />

          <div class="flex items-center gap-2">
            <Checkbox
              :model-value="form.sign"
              @update:model-value="onSignChange"
            />
            <Label class="text-sm">
              {{ t("notificationSettings.signBody") }}
            </Label>
          </div>
          <FormField
            v-if="form.sign"
            v-model="form.signingSecret"
            type="password"
            :label="t('notificationSettings.signingSecret')"
            :hint="
              editingChannel
                ? t('notificationSettings.secretKeepHint')
                : t('notificationSettings.signingSecretHint')
            "
          />

          <div class="grid gap-4 sm:grid-cols-2">
            <FormField :label="t('notificationSettings.minSeverity')">
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
            </FormField>
            <FormField :label="t('notificationSettings.language')">
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
            </FormField>
          </div>

          <FormField :label="t('notificationSettings.eventTypes')">
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
          </FormField>

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
  NotificationChannelKind,
  NotificationChannelSchema,
  NotificationChannelSecretSchema,
  type NotificationDelivery,
  NotificationDeliveryState,
  NotificationSeverity,
  NotificationType,
} from "@/types/proto-es/v1/notification_service_pb";
import { formatDateTime } from "@/utils/datetime";

/**
 * Outbound notification channels. A channel is a destination an administrator
 * configures; every in-app message an event produces is posted to it once. The
 * credential (the endpoint URL, and the HMAC key when signing) is write-only:
 * the read reports only whether one is stored, so editing keeps it unless the
 * field is filled in again.
 */

const { t, locale } = useI18n();
const authStore = useAuthStore();
const { formatError } = useErrorHandler();

const canUpdate = computed(() =>
  authStore.hasPermission("metaxisdata.settings.update")
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
  endpoint: string;
  signingSecret: string;
  sign: boolean;
  enabled: boolean;
  eventTypes: NotificationType[];
  minSeverity: NotificationSeverity;
  language: string;
}

const form = reactive<ChannelForm>({
  displayName: "",
  endpoint: "",
  signingSecret: "",
  sign: false,
  enabled: true,
  eventTypes: [],
  minSeverity: NotificationSeverity.UNSPECIFIED,
  language: locale.value === "zh-CN" ? "zh-CN" : "en-US",
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

function resetForm() {
  form.displayName = "";
  form.endpoint = "";
  form.signingSecret = "";
  form.sign = false;
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
  form.endpoint = "";
  form.signingSecret = "";
  form.sign =
    channel.config?.config?.case === "webhook"
      ? Boolean(channel.config.config.value.sign)
      : false;
  form.enabled = channel.enabled;
  form.eventTypes = [...channel.eventTypes];
  form.minSeverity = channel.minSeverity;
  form.language = channel.language || "en-US";
  dialogOpen.value = true;
}

function onDeliveryFilter(value: unknown) {
  setDeliveryFilter(String(value));
}

function onSignChange(value: boolean | "indeterminate") {
  form.sign = value === true;
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

function buildChannel(): NotificationChannel {
  return create(NotificationChannelSchema, {
    name: editingChannel.value?.name ?? "",
    displayName: form.displayName.trim(),
    kind: NotificationChannelKind.WEBHOOK,
    enabled: form.enabled,
    eventTypes: form.eventTypes,
    minSeverity: form.minSeverity,
    language: form.language,
    config: {
      config: {
        case: "webhook",
        value: { sign: form.sign },
      },
    },
  });
}

function buildSecret() {
  const endpoint = form.endpoint.trim();
  const signingSecret = form.signingSecret;
  if (!endpoint && !signingSecret) {
    return undefined;
  }
  return create(NotificationChannelSecretSchema, {
    webhookUrl: endpoint,
    webhookSigningSecret: signingSecret,
  });
}

async function saveChannel() {
  if (form.displayName.trim() === "") {
    notify.error(t("notificationSettings.nameRequired"));
    return;
  }
  if (!editingChannel.value && form.endpoint.trim() === "") {
    notify.error(t("notificationSettings.endpointRequired"));
    return;
  }
  if (form.sign && !form.signingSecret && !editingChannel.value?.secretSet) {
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
  await Promise.all([loadChannels(), loadDeliveries(true)]);
});
</script>
