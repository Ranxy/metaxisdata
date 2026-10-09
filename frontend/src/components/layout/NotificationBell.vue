<template>
  <DropdownMenu v-model:open="open">
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
      >
        <span class="relative">
          <Bell class="h-5 w-5 shrink-0" />
          <!-- The dot is the only indicator the collapsed rail can carry; its
               count is unreadable at 16px, so it is a dot there and a number in
               the dropdown and the expanded row. It marks messages the user has
               not seen since opening the inbox, not unread messages: opening the
               dropdown clears it while every message keeps its unread state. -->
          <span
            v-if="store.hasUnseen"
            class="absolute -top-1 -right-1 h-2.5 w-2.5 rounded-full bg-destructive"
            aria-hidden="true"
          />
        </span>

        <template v-if="!rail">
          <span class="min-w-0 flex-1 truncate text-left text-sm font-medium">
            {{ t("notifications.title") }}
          </span>
          <Badge
            v-if="store.hasUnseen"
            variant="destructive"
            class="shrink-0 tabular-nums"
          >
            {{ unseenLabel }}
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
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
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

const unseenLabel = computed(() =>
  store.unseenCount > 99 ? "99+" : String(store.unseenCount)
);

function text(item: Notification) {
  return describeNotification(item);
}

const open = ref(false);

// Opening the dropdown is one of the two ways a user opens notifications, so it is what
// clears the badge: the store tells the server and keeps every message unread, and a
// message that arrives while the list is on screen does not light the badge again. The
// list is loaded here too, because the count is the one thing a glance has to deliver
// even when the dropdown opens before the first stream message arrived. A failed load is
// not worth a toast: the page reports it, and the next reconnect retries.
watch(open, (isOpen) => {
  if (isOpen) {
    store.openInbox();
    void store.refreshRecent().catch(() => {});
    return;
  }
  store.closeInbox();
});

// The sidebar exists only inside the authenticated shell, so it owns the stream:
// there is nothing to notify a signed-out visitor about, and the sidebar's own
// lifetime is exactly the session's.
onMounted(() => store.startStreaming());
onBeforeUnmount(() => {
  // A menu that unmounts while it is open never reports that it closed, and the surface
  // count is what keeps a new message from lighting the badge.
  if (open.value) {
    store.closeInbox();
  }
  store.stopStreaming();
});

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
