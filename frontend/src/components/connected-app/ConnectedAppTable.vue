<template>
  <Table>
    <TableHeader>
      <TableRow>
        <TableHead>{{ t("connectedApps.app") }}</TableHead>
        <TableHead v-if="showUser">
          {{ t("connectedApps.user") }}
        </TableHead>
        <TableHead class="whitespace-nowrap">
          {{ t("connectedApps.authorizedAt") }}
        </TableHead>
        <TableHead class="whitespace-nowrap">
          {{ t("connectedApps.lastToolCallAt") }}
        </TableHead>
        <TableHead class="whitespace-nowrap">
          {{ t("connectedApps.expiresAt") }}
        </TableHead>
        <TableHead>{{ t("connectedApps.status") }}</TableHead>
        <TableHead class="text-right">
          {{ t("common.actions") }}
        </TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      <TableRow
        v-for="app in apps"
        :key="app.name"
      >
        <TableCell>
          <!-- The registered name is self-reported and unverified, so it is plain
               text; when it is missing (or the registration was deleted) the
               opaque client id is the honest label. -->
          <div class="font-medium break-words">
            {{ app.clientName || app.clientId }}
          </div>
          <div
            v-if="app.clientName"
            class="text-xs break-all text-muted-foreground"
          >
            {{ app.clientId }}
          </div>
        </TableCell>
        <TableCell v-if="showUser" class="text-sm">
          <!-- user_email is filled only for the caller's own rows or when the
               caller may read users; the resource name is the fallback. -->
          <span class="break-all">{{ app.userEmail || app.user }}</span>
        </TableCell>
        <TableCell class="whitespace-nowrap text-sm">
          {{ formatDateTime(app.createTime, locale) }}
        </TableCell>
        <TableCell class="whitespace-nowrap text-sm">
          <span
            v-if="app.lastToolCallTime"
            :title="formatDateTime(app.lastToolCallTime, locale)"
          >
            {{ formatRelativeTime(app.lastToolCallTime, locale) }}
          </span>
          <span v-else class="text-muted-foreground">
            {{ t("connectedApps.noToolCall") }}
          </span>
        </TableCell>
        <TableCell class="whitespace-nowrap text-sm">
          {{ formatDateTime(app.expireTime, locale) }}
        </TableCell>
        <TableCell>
          <Badge :variant="statusVariant(app.state)">
            {{ statusLabel(app.state) }}
          </Badge>
        </TableCell>
        <TableCell class="text-right">
          <Button
            v-if="isRevokable(app.state)"
            variant="outline"
            size="sm"
            @click="emit('revoke', app)"
          >
            {{ t("connectedApps.revoke") }}
          </Button>
          <!-- A terminal row keeps the action slot occupied so the column stays
               aligned; a `span` carries the tooltip because a disabled button
               does not receive the pointer events a native tooltip needs. -->
          <span
            v-else
            class="inline-flex h-9 w-9 items-center justify-center text-muted-foreground/30"
            :title="statusLabel(app.state)"
          >
            <Ban class="h-4 w-4" />
          </span>
        </TableCell>
      </TableRow>
    </TableBody>
  </Table>
</template>

<script setup lang="ts">
import { Ban } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  type ConnectedApp,
  ConnectedApp_State,
} from "@/types/proto-es/v1/connected_app_service_pb";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

type BadgeVariant = "success" | "warning" | "secondary" | "outline";

withDefaults(
  defineProps<{
    apps: ConnectedApp[];
    /** Administrators see whose connection each row is; self-service does not. */
    showUser?: boolean;
  }>(),
  { showUser: false }
);

const emit = defineEmits<{
  revoke: [app: ConnectedApp];
}>();

const { t, locale } = useI18n();

/** Red is reserved for failures and the confirm button; a state is a tone. */
function statusVariant(state: ConnectedApp_State): BadgeVariant {
  switch (state) {
    case ConnectedApp_State.ACTIVE:
      return "success";
    case ConnectedApp_State.EXPIRED:
    case ConnectedApp_State.REAUTH_REQUIRED:
      return "warning";
    case ConnectedApp_State.REVOKED:
      return "secondary";
    default:
      return "outline";
  }
}

function statusLabel(state: ConnectedApp_State): string {
  switch (state) {
    case ConnectedApp_State.ACTIVE:
      return t("connectedApps.statusActive");
    case ConnectedApp_State.REVOKED:
      return t("connectedApps.statusRevoked");
    case ConnectedApp_State.EXPIRED:
      return t("connectedApps.statusExpired");
    case ConnectedApp_State.REAUTH_REQUIRED:
      return t("connectedApps.statusReauthRequired");
    case ConnectedApp_State.INACTIVE:
      return t("connectedApps.statusInactive");
    default:
      return t("connectedApps.statusUnknown");
  }
}

/**
 * Revoking stops what could replace the credential, so it only makes sense while
 * there is access left to cut. A re-authorization-required row still holds a
 * grant and can be revoked; revoked, expired and deactivated ones cannot.
 */
function isRevokable(state: ConnectedApp_State): boolean {
  return (
    state === ConnectedApp_State.ACTIVE ||
    state === ConnectedApp_State.REAUTH_REQUIRED
  );
}
</script>
