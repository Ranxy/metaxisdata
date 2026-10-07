<template>
  <div class="space-y-4">
    <PageHeader
      :title="t('notifications.title')"
      :description="t('notifications.pageDescription')"
    >
      <template #actions>
        <Button
          variant="outline"
          :disabled="isLoading"
          @click="refresh"
        >
          <RefreshCcw
            class="mr-2 h-4 w-4"
            :class="{ 'animate-spin': isLoading }"
          />
          {{ t("notifications.refresh") }}
        </Button>
        <Button
          :disabled="!store.hasUnread"
          @click="markAllRead"
        >
          <CheckCheck class="mr-2 h-4 w-4" />
          {{ t("notifications.markAllRead") }}
        </Button>
      </template>
    </PageHeader>

    <!-- Two filters rather than a search box: an inbox is read or unread, and
         the server answers the unread one from a partial index. -->
    <div class="flex items-center gap-2">
      <Button
        size="sm"
        :variant="unreadOnly ? 'outline' : 'secondary'"
        @click="setFilter(false)"
      >
        {{ t("notifications.filterAll") }}
      </Button>
      <Button
        size="sm"
        :variant="unreadOnly ? 'secondary' : 'outline'"
        @click="setFilter(true)"
      >
        {{ t("notifications.filterUnread") }}
      </Button>
    </div>

    <Card>
      <CardContent class="space-y-4">
        <PageState
          :loading="isLoading"
          :error="error"
        >
          <EmptyState
            v-if="notifications.length === 0"
            :icon="BellOff"
            :title="unreadOnly ? t('notifications.emptyUnreadTitle') : t('notifications.emptyTitle')"
            :description="unreadOnly ? t('notifications.emptyUnreadDescription') : t('notifications.emptyDescription')"
          />

          <ul
            v-else
            class="divide-y"
          >
            <li
              v-for="notification in notifications"
              :key="notification.name"
              class="flex gap-3 py-4"
            >
              <component
                :is="severityIcon(notification.severity)"
                class="mt-0.5 h-5 w-5 shrink-0"
                :class="severityTone(notification.severity)"
              />

              <div class="min-w-0 flex-1 space-y-1">
                <div class="flex flex-wrap items-center gap-2">
                  <p class="text-sm font-medium">
                    {{ t(textOf(notification).titleKey, textOf(notification).titleParams) }}
                  </p>
                  <Badge
                    v-if="!notification.readTime"
                    variant="secondary"
                    class="text-[10px]"
                  >
                    {{ t("notifications.statusUnread") }}
                  </Badge>
                </div>

                <p class="text-sm text-muted-foreground">
                  {{ t(textOf(notification).messageKey, textOf(notification).messageParams) }}
                </p>

                <!-- The facts the body line deliberately leaves out. A sync
                     reports the databases that did not succeed; an ingestion
                     failure names the namespace, the key and the raw error. -->
                <template v-if="schemaOf(notification)">
                  <p
                    v-if="schemaOf(notification)?.instanceError"
                    class="text-xs text-muted-foreground"
                  >
                    {{
                      t("notifications.schemaSyncInstanceError", {
                        error: schemaOf(notification)?.instanceError,
                      })
                    }}
                  </p>
                  <ul
                    v-if="listedDatabases(notification).length > 0"
                    class="space-y-0.5 pt-1"
                  >
                    <li
                      v-for="database in listedDatabases(notification)"
                      :key="database.database"
                      class="text-xs"
                    >
                      <span class="font-mono">{{ databaseLabel(database.database) }}</span>
                      <span class="text-muted-foreground">
                        — {{ databaseOutcome(database) }}
                      </span>
                    </li>
                  </ul>
                  <p
                    v-if="hiddenDatabaseCount(notification) > 0"
                    class="text-xs text-muted-foreground"
                  >
                    {{
                      t("notifications.schemaSyncMoreDatabases", {
                        count: hiddenDatabaseCount(notification),
                      })
                    }}
                  </p>
                </template>

                <div
                  v-if="openLineageOf(notification)"
                  class="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground"
                >
                  <span v-if="openLineageOf(notification)?.namespace">
                    {{
                      t("notifications.openlineageNamespace", {
                        namespace: openLineageOf(notification)?.namespace,
                      })
                    }}
                  </span>
                  <span v-if="openLineageOf(notification)?.apiKey">
                    {{
                      t("notifications.openlineageKey", {
                        apiKey: openLineageOf(notification)?.apiKey,
                      })
                    }}
                  </span>
                  <span v-if="openLineageOf(notification)?.receivedCount">
                    {{
                      t("notifications.openlineageCounts", {
                        failed: openLineageOf(notification)?.failedCount,
                        received: openLineageOf(notification)?.receivedCount,
                      })
                    }}
                  </span>
                </div>

                <p
                  v-if="detailError(notification)"
                  class="font-mono text-xs break-all text-muted-foreground"
                >
                  {{ detailError(notification) }}
                </p>

                <div class="flex flex-wrap items-center gap-x-3 gap-y-1 pt-1 text-xs text-muted-foreground">
                  <span>{{ formatRelativeTime(notification.createTime, locale) }}</span>
                  <Button
                    v-if="!notification.readTime"
                    variant="link"
                    size="sm"
                    class="h-auto p-0 text-xs"
                    @click="markRead(notification)"
                  >
                    {{ t("notifications.markRead") }}
                  </Button>
                  <RouterLink
                    v-if="textOf(notification).href"
                    :to="textOf(notification).href!"
                    class="underline-offset-4 hover:text-foreground hover:underline"
                  >
                    {{ t("notifications.open") }}
                  </RouterLink>
                  <Button
                    variant="link"
                    size="sm"
                    class="h-auto p-0 text-xs text-destructive"
                    @click="askDelete(notification)"
                  >
                    {{ t("notifications.delete") }}
                  </Button>
                </div>
              </div>
            </li>
          </ul>
        </PageState>

        <TablePager
          v-if="notifications.length > 0 || hasPrevious"
          :page-size="pageSize"
          :has-previous="hasPrevious"
          :has-next="hasNext"
          :disabled="isLoading"
          @update:page-size="setPageSize"
          @previous="goPrevious"
          @next="goNext"
        />
      </CardContent>
    </Card>

    <ConfirmDeleteDialog
      v-model="showDeleteDialog"
      :title="t('notifications.deleteTitle')"
      :message="t('notifications.deleteConfirmMessage')"
      :item-name="deleting ? t(textOf(deleting).titleKey, textOf(deleting).titleParams) : ''"
      :loading="isDeleting"
      @confirm="confirmDelete"
    />
  </div>
</template>

<script setup lang="ts">
import {
  AlertTriangle,
  BellOff,
  CheckCheck,
  CircleAlert,
  Info,
  RefreshCcw,
} from "lucide-vue-next";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listNotificationsPage } from "@/api/notification";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { databaseLabel, describeNotification } from "@/lib/notificationText";
import { useNotificationStore } from "@/store/modules/notification";
import {
  type Notification,
  NotificationSeverity,
  type OpenLineageDetail,
  type SchemaSyncDetail,
  type SyncDatabaseResult,
  SyncDatabaseState,
} from "@/types/proto-es/v1/notification_service_pb";
import { formatRelativeTime } from "@/utils/datetime";

/** How many databases one row lists before it summarizes the rest. */
const MAX_LISTED_DATABASES = 5;

const { t, locale } = useI18n();
const { handleError, showSuccess } = useErrorHandler();
const store = useNotificationStore();

const unreadOnly = ref(false);
const error = ref("");
const pageSize = ref(20);

const {
  items: notifications,
  isLoading,
  hasNext,
  hasPrevious,
  refresh,
  goNext,
  goPrevious,
} = usePagedFetch<Notification>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    return await listNotificationsPage({
      pageSize: pageSize.value,
      pageToken,
      unreadOnly: unreadOnly.value,
      signal,
    });
  },
  onError: (err) => {
    error.value = handleError(err, "notifications.fetchError");
  },
});

// A bigger page restarts the walk: a cursor belongs to the query that produced
// it, and the server refuses a token whose parameters changed.
watch(pageSize, () => {
  void refresh();
});

function textOf(notification: Notification) {
  return describeNotification(notification);
}

function schemaOf(notification: Notification): SchemaSyncDetail | undefined {
  return notification.detail.case === "schemaSync"
    ? notification.detail.value
    : undefined;
}

function openLineageOf(
  notification: Notification
): OpenLineageDetail | undefined {
  return notification.detail.case === "openlineage"
    ? notification.detail.value
    : undefined;
}

function listedDatabases(notification: Notification): SyncDatabaseResult[] {
  return schemaOf(notification)?.databases.slice(0, MAX_LISTED_DATABASES) ?? [];
}

function hiddenDatabaseCount(notification: Notification): number {
  const listed = schemaOf(notification)?.databases.length ?? 0;
  return Math.max(0, listed - MAX_LISTED_DATABASES);
}

/**
 * What happened to one database. A failed entry carries the driver's own error,
 * which is the only diagnostic the user has; the other two states read as words
 * because there is nothing else to say about them.
 */
function databaseOutcome(database: SyncDatabaseResult): string {
  switch (database.state) {
    case SyncDatabaseState.SUCCEEDED:
      return t("notifications.schemaSyncDatabaseSucceeded");
    case SyncDatabaseState.UNFINISHED:
      return t("notifications.schemaSyncUnfinished");
    case SyncDatabaseState.FAILED:
      return database.error;
    case SyncDatabaseState.UNSPECIFIED:
      return database.error;
    default:
      return database.error;
  }
}

/** The raw error behind an ingestion failure, when the report carries one. */
function detailError(notification: Notification): string {
  return openLineageOf(notification)?.error ?? "";
}

function severityIcon(severity: NotificationSeverity) {
  switch (severity) {
    case NotificationSeverity.ERROR:
      return CircleAlert;
    case NotificationSeverity.WARNING:
      return AlertTriangle;
    case NotificationSeverity.INFO:
      return Info;
    case NotificationSeverity.UNSPECIFIED:
      return Info;
    default:
      return Info;
  }
}

function severityTone(severity: NotificationSeverity) {
  switch (severity) {
    case NotificationSeverity.ERROR:
      return "text-destructive";
    case NotificationSeverity.WARNING:
      return "text-yellow-600";
    case NotificationSeverity.INFO:
      return "text-muted-foreground";
    case NotificationSeverity.UNSPECIFIED:
      return "text-muted-foreground";
    default:
      return "text-muted-foreground";
  }
}

function setFilter(onlyUnread: boolean) {
  if (unreadOnly.value === onlyUnread) {
    return;
  }
  unreadOnly.value = onlyUnread;
  void refresh();
}

function setPageSize(size: number) {
  pageSize.value = size;
}

async function markRead(notification: Notification) {
  try {
    await store.markRead([notification.name]);
    await refresh();
  } catch (err) {
    handleError(err, "notifications.actionError");
  }
}

async function markAllRead() {
  try {
    await store.markAllRead();
    showSuccess("notifications.markAllReadSuccess");
    await refresh();
  } catch (err) {
    handleError(err, "notifications.actionError");
  }
}

const showDeleteDialog = ref(false);
const isDeleting = ref(false);
const deleting = ref<Notification | undefined>();

function askDelete(notification: Notification) {
  deleting.value = notification;
  showDeleteDialog.value = true;
}

async function confirmDelete() {
  const notification = deleting.value;
  if (!notification) {
    return;
  }
  isDeleting.value = true;
  try {
    await store.remove(notification.name);
    showSuccess("notifications.deleteSuccess");
    showDeleteDialog.value = false;
    deleting.value = undefined;
    await refresh();
  } catch (err) {
    handleError(err, "notifications.actionError");
  } finally {
    isDeleting.value = false;
  }
}
</script>
