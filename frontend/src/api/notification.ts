import { create } from "@bufbuild/protobuf";
import {
  BatchMarkNotificationsReadRequestSchema,
  DeleteNotificationRequestSchema,
  GetUnreadNotificationCountRequestSchema,
  ListNotificationsRequestSchema,
  MarkAllNotificationsReadRequestSchema,
  MarkNotificationsSeenRequestSchema,
  type Notification,
  SubscribeNotificationsRequestSchema,
} from "@/types/proto-es/v1/notification_service_pb";
import { notificationClient } from "./client";
import type { ListPage } from "./list";

/**
 * The workspace the inbox belongs to. The server scopes every query to the
 * authenticated caller, so the parent only names the resource; `-` is the
 * shorthand for "the current workspace".
 */
const WORKSPACE_PARENT = "workspaces/-";

/** One page of the caller's notifications, newest first. */
export async function listNotificationsPage(options: {
  pageSize: number;
  pageToken?: string;
  unreadOnly?: boolean;
  signal?: AbortSignal;
}): Promise<ListPage<Notification>> {
  const response = await notificationClient.listNotifications(
    create(ListNotificationsRequestSchema, {
      parent: WORKSPACE_PARENT,
      pageSize: options.pageSize,
      pageToken: options.pageToken ?? "",
      unreadOnly: options.unreadOnly ?? false,
    }),
    { signal: options.signal }
  );
  return {
    items: response.notifications,
    nextPageToken: response.nextPageToken,
  };
}

/** The handful of messages the notification bell shows. */
export async function listRecentNotifications(
  pageSize = 8,
  signal?: AbortSignal
): Promise<Notification[]> {
  const page = await listNotificationsPage({ pageSize, signal });
  return page.items;
}

/** The two numbers the inbox surfaces read: everything unread, and the badge. */
export interface NotificationCounts {
  unreadCount: number;
  unseenCount: number;
}

/**
 * Both counts of the caller's inbox. `unreadCount` is what the inbox page works with;
 * `unseenCount` is the red badge, and it is the unread messages written since the
 * caller last opened the inbox.
 */
export async function getNotificationCounts(
  signal?: AbortSignal
): Promise<NotificationCounts> {
  const response = await notificationClient.getUnreadNotificationCount(
    create(GetUnreadNotificationCountRequestSchema, {
      parent: WORKSPACE_PARENT,
    }),
    { signal }
  );
  return {
    unreadCount: response.unreadCount,
    unseenCount: response.unseenCount,
  };
}

/**
 * Records that the caller has opened the inbox, which clears the badge. No message is
 * marked read: each one keeps its unread state until it is clicked.
 */
export async function markNotificationsSeen(): Promise<void> {
  await notificationClient.markNotificationsSeen(
    create(MarkNotificationsSeenRequestSchema, {
      parent: WORKSPACE_PARENT,
    })
  );
}

export async function markNotificationsRead(names: string[]): Promise<void> {
  await notificationClient.batchMarkNotificationsRead(
    create(BatchMarkNotificationsReadRequestSchema, {
      parent: WORKSPACE_PARENT,
      names,
    })
  );
}

export async function markAllNotificationsRead(): Promise<void> {
  await notificationClient.markAllNotificationsRead(
    create(MarkAllNotificationsReadRequestSchema, {
      parent: WORKSPACE_PARENT,
    })
  );
}

export async function deleteNotification(name: string): Promise<void> {
  await notificationClient.deleteNotification(
    create(DeleteNotificationRequestSchema, { name })
  );
}

/**
 * The caller's live notifications, newest last. The stream is the push channel:
 * the server sends a message as it is written, and a `keepAlive` so an idle
 * connection is not closed by a proxy. It runs until `signal` aborts or the
 * server ends it — reconnecting is the caller's business, because only it knows
 * what the inbox on screen still needs.
 */
export function subscribeNotifications(signal?: AbortSignal) {
  return notificationClient.subscribeNotifications(
    create(SubscribeNotificationsRequestSchema, { parent: WORKSPACE_PARENT }),
    { signal }
  );
}
