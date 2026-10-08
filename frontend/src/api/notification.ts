import { create } from "@bufbuild/protobuf";
import {
  BatchMarkNotificationsReadRequestSchema,
  DeleteNotificationRequestSchema,
  GetUnreadNotificationCountRequestSchema,
  ListNotificationsRequestSchema,
  MarkAllNotificationsReadRequestSchema,
  type Notification,
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

export async function getUnreadNotificationCount(
  signal?: AbortSignal
): Promise<number> {
  const response = await notificationClient.getUnreadNotificationCount(
    create(GetUnreadNotificationCountRequestSchema, {
      parent: WORKSPACE_PARENT,
    }),
    { signal }
  );
  return response.unreadCount;
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
