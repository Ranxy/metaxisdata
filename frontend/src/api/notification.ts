import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  BatchMarkNotificationsReadRequestSchema,
  CreateNotificationChannelRequestSchema,
  CreateNotificationRouteRequestSchema,
  DeleteNotificationChannelRequestSchema,
  DeleteNotificationRequestSchema,
  DeleteNotificationRouteRequestSchema,
  GetUnreadNotificationCountRequestSchema,
  ListNotificationChannelsRequestSchema,
  ListNotificationDeliveriesRequestSchema,
  ListNotificationRoutesRequestSchema,
  ListNotificationsRequestSchema,
  MarkAllNotificationsReadRequestSchema,
  MarkNotificationsSeenRequestSchema,
  type Notification,
  type NotificationChannel,
  type NotificationChannelSecret,
  type NotificationDelivery,
  NotificationDeliveryState,
  type NotificationRoute,
  RetryNotificationDeliveryRequestSchema,
  SubscribeNotificationsRequestSchema,
  TestNotificationChannelRequestSchema,
  UpdateNotificationChannelRequestSchema,
  UpdateNotificationRouteRequestSchema,
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

// ---- Outbound channels and deliveries -------------------------------------
//
// A channel is an administrator-configured destination (a webhook today); a
// delivery is one queued attempt at one notification event for one channel. The
// credential is write-only: reads report only whether one is stored.

/** Every configured outbound channel. */
export async function listNotificationChannels(
  signal?: AbortSignal
): Promise<NotificationChannel[]> {
  const response = await notificationClient.listNotificationChannels(
    create(ListNotificationChannelsRequestSchema, { parent: WORKSPACE_PARENT }),
    { signal }
  );
  return response.notificationChannels;
}

/** Create one channel together with its write-only credential. */
export async function createNotificationChannel(
  channel: NotificationChannel,
  secret?: NotificationChannelSecret
): Promise<NotificationChannel> {
  return await notificationClient.createNotificationChannel(
    create(CreateNotificationChannelRequestSchema, {
      parent: WORKSPACE_PARENT,
      channel,
      secret,
    })
  );
}

/** Update the fields named by `updateMask`; an omitted secret keeps the stored one. */
export async function updateNotificationChannel(
  channel: NotificationChannel,
  updateMask: string[],
  secret?: NotificationChannelSecret
): Promise<NotificationChannel> {
  return await notificationClient.updateNotificationChannel(
    create(UpdateNotificationChannelRequestSchema, {
      channel,
      secret,
      updateMask: create(FieldMaskSchema, { paths: updateMask }),
    })
  );
}

/** Delete one channel and the deliveries still queued for it. */
export async function deleteNotificationChannel(name: string): Promise<void> {
  await notificationClient.deleteNotificationChannel(
    create(DeleteNotificationChannelRequestSchema, { name })
  );
}

/** Post one test message through a stored channel and return its answer. */
export async function testNotificationChannel(name: string): Promise<string> {
  const response = await notificationClient.testNotificationChannel(
    create(TestNotificationChannelRequestSchema, { name })
  );
  return response.response;
}

/** One page of outbound deliveries, newest first. */
export async function listNotificationDeliveriesPage(options: {
  pageSize: number;
  pageToken?: string;
  channel?: string;
  state?: NotificationDeliveryState;
  signal?: AbortSignal;
}): Promise<ListPage<NotificationDelivery>> {
  const response = await notificationClient.listNotificationDeliveries(
    create(ListNotificationDeliveriesRequestSchema, {
      parent: WORKSPACE_PARENT,
      pageSize: options.pageSize,
      pageToken: options.pageToken ?? "",
      channel: options.channel ?? "",
      state: options.state ?? NotificationDeliveryState.UNSPECIFIED,
    }),
    { signal: options.signal }
  );
  return {
    items: response.notificationDeliveries,
    nextPageToken: response.nextPageToken,
  };
}

/** Requeue one DEAD delivery for another attempt. */
export async function retryNotificationDelivery(
  name: string
): Promise<NotificationDelivery> {
  return await notificationClient.retryNotificationDelivery(
    create(RetryNotificationDeliveryRequestSchema, { name })
  );
}

// ---- Routing rules ---------------------------------------------------------
//
// A route is an owner/team rule: when an event's in-app audience matches, the
// route's channels receive it even when they are routed_only. A channel that is
// not routed_only keeps receiving events from its own subscription.

/** Every configured routing rule. The list is not paged. */
export async function listNotificationRoutes(
  signal?: AbortSignal
): Promise<NotificationRoute[]> {
  const response = await notificationClient.listNotificationRoutes(
    create(ListNotificationRoutesRequestSchema, { parent: WORKSPACE_PARENT }),
    { signal }
  );
  return response.notificationRoutes;
}

/** Create one routing rule under the current workspace. */
export async function createNotificationRoute(
  route: NotificationRoute
): Promise<NotificationRoute> {
  return await notificationClient.createNotificationRoute(
    create(CreateNotificationRouteRequestSchema, {
      parent: WORKSPACE_PARENT,
      route,
    })
  );
}

/** Update the fields named by `updateMask`; an empty mask writes every mutable field. */
export async function updateNotificationRoute(
  route: NotificationRoute,
  updateMask: string[]
): Promise<NotificationRoute> {
  return await notificationClient.updateNotificationRoute(
    create(UpdateNotificationRouteRequestSchema, {
      route,
      updateMask: create(FieldMaskSchema, { paths: updateMask }),
    })
  );
}

/** Delete one routing rule. Its channels keep their own subscriptions. */
export async function deleteNotificationRoute(name: string): Promise<void> {
  await notificationClient.deleteNotificationRoute(
    create(DeleteNotificationRouteRequestSchema, { name })
  );
}
