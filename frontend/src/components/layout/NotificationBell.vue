<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button
        variant="ghost"
        :title="rail ? t('notifications.title') : undefined"
        :aria-label="rail ? t('notifications.title') : undefined"
        :class="
          rail
            ? 'mx-auto h-10 w-10 justify-center p-0'
            : 'h-auto w-full justify-start gap-2 px-2 py-2'
        "
        @click="refreshRecent"
      >
        <span class="relative">
          <Bell class="h-5 w-5 shrink-0" />
          <!-- The dot is the only indicator the collapsed rail can carry; its
               count is unreadable at 16px, so it is a dot there and a number in
               the dropdown and the expanded row. -->
          <span
            v-if="store.hasUnread"
            class="absolute -top-1 -right-1 h-2.5 w-2.5 rounded-full bg-destructive"
            aria-hidden="true"
          />
        </span>

        <template v-if="!rail">
          <span class="min-w-0 flex-1 truncate text-left text-sm font-medium">
            {{ t("notifications.title") }}
          </span>
          <Badge
            v-if="store.hasUnread"
            variant="destructive"
            class="shrink-0 tabular-nums"
          >
            {{ unreadLabel }}
          </Badge>
        </template>
      </Button>
    </DropdownMenuTrigger>

    <DropdownMenuContent
      side="top"
      align="start"
      class="w-80"
    >
      <DropdownMenuLabel class="font-normal">
        {{ t("notifications.title") }}
      </DropdownMenuLabel>
      <DropdownMenuSeparator />

      <p
        v-if="store.loadingRecent"
        class="px-4 py-6 text-center text-sm text-muted-foreground"
      >
        {{ t("common.loading") }}
      </p>

      <EmptyState
        v-else-if="store.recent.length === 0"
        :icon="BellOff"
        :title="t('notifications.emptyTitle')"
        :description="t('notifications.emptyDescription')"
      />

      <template v-else>
        <DropdownMenuItem
          v-for="item in store.recent"
          :key="item.name"
          class="flex items-start gap-2"
          @select="openNotification(item)"
        >
          <span
            class="mt-1.5 h-2 w-2 shrink-0 rounded-full"
            :class="item.readTime ? 'bg-transparent' : 'bg-primary'"
            aria-hidden="true"
          />
          <span class="min-w-0 flex-1 space-y-0.5">
            <span class="block truncate text-sm font-medium">
              {{ t(text(item).titleKey, text(item).titleParams) }}
            </span>
            <span class="block truncate text-xs text-muted-foreground">
              {{ t(text(item).messageKey, text(item).messageParams) }}
            </span>
          </span>
          <span class="shrink-0 pt-0.5 text-xs text-muted-foreground">
            {{ formatRelativeTime(item.createTime, locale) }}
          </span>
        </DropdownMenuItem>
      </template>

      <DropdownMenuSeparator />
      <DropdownMenuItem as-child>
        <RouterLink
          to="/notifications"
          class="justify-center text-sm"
        >
          {{ t("notifications.viewAll") }}
        </RouterLink>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>

<script setup lang="ts">
import { Bell, BellOff } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import EmptyState from "@/components/common/EmptyState.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { describeNotification } from "@/lib/notificationText";
import { useAppStore } from "@/store/modules/app";
import { useNotificationStore } from "@/store/modules/notification";
import type { Notification } from "@/types/proto-es/v1/notification_service_pb";
import { formatRelativeTime } from "@/utils/datetime";

// The rail hides the row's label, so the bell borrows the sidebar's own width
// rules: a centred square in the rail, an account-row-shaped button otherwise.
const appStore = useAppStore();
const rail = computed(() => appStore.sidebarCollapsed);

const { t, locale } = useI18n();
const router = useRouter();
const store = useNotificationStore();

const unreadLabel = computed(() =>
  store.unreadCount > 99 ? "99+" : String(store.unreadCount)
);

function text(item: Notification) {
  return describeNotification(item);
}

// The count is the one thing a glance has to deliver, so the trigger loads it
// even when the dropdown was opened before the first poll answered. A failed
// load is not worth a toast here: the page reports it, and the poll retries.
function refreshRecent() {
  void store.refreshRecent().catch(() => {});
}

// The sidebar exists only inside the authenticated shell, so it owns the poll:
// there is nothing to notify a signed-out visitor about, and the sidebar's own
// lifetime is exactly the session's.
onMounted(() => store.startPolling());
onBeforeUnmount(() => store.stopPolling());

function openNotification(item: Notification) {
  if (!item.readTime) {
    void store.markRead([item.name]).catch(() => {});
  }
  const href = text(item).href;
  if (href) {
    void router.push(href);
  }
}
</script>
