import { create } from "@bufbuild/protobuf";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  NotificationService,
  SubscribeNotificationsRequestSchema,
  type SubscribeNotificationsResponse,
} from "@/types/proto-es/v1/notification_service_pb";
import {
  sessionInterceptor,
  setSessionRefresher,
  setUnauthenticatedHandler,
} from "./session";

/**
 * These exercise the real transport and the real interceptor against a mocked fetch, so
 * what they prove is the behaviour the notification store depends on: a server-streaming
 * call whose handshake is refused as an expired session is caught by the session
 * interceptor, the session is renewed, and the connect loop's next reconnect carries the
 * renewed cookie. A renewal that cannot happen ends the session instead of leaving the
 * loop to retry a dead credential.
 *
 * connect-web surfaces the refusal at the initial call, because the transport awaits the
 * response before it hands back the iterable (`await fetch` then `validateResponse` in
 * connect-transport.js) — that is what the first test pins. It also pins what the store is
 * designed around: the interceptor's single replay cannot rebuild a server-streaming
 * request, because connect-web builds the body by consuming the input as a one-shot async
 * iterable, so the replay fails locally with "missing request message" rather than
 * repeating the 401. The renewal has already happened by then, the session is deliberately
 * not ended, and the next reconnect — a fresh call, which is what the store does — works.
 */

/** One Connect streaming envelope: a flags byte, a big-endian length, then the payload. */
function envelope(flags: number, payload: string): Uint8Array {
  const bytes = new TextEncoder().encode(payload);
  const frame = new Uint8Array(5 + bytes.length);
  frame[0] = flags;
  new DataView(frame.buffer).setUint32(1, bytes.length);
  frame.set(bytes, 5);
  return frame;
}

const MESSAGE = JSON.stringify({
  notification: {
    notification: {
      name: "workspaces/ws/notifications/1",
      type: "NOTIFICATION_TYPE_SCHEMA_SYNC",
      severity: "NOTIFICATION_SEVERITY_INFO",
    },
    unreadCount: 3,
  },
});

/** A message envelope followed by the end-of-stream envelope the protocol requires. */
function streamed(): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(envelope(0x00, MESSAGE));
      controller.enqueue(envelope(0x02, "{}"));
      controller.close();
    },
  });
  return {
    status: 200,
    headers: new Headers({ "content-type": "application/connect+json" }),
    body,
  } as unknown as Response;
}

function unauthorized(): Response {
  return {
    status: 401,
    headers: new Headers({ "content-type": "application/json" }),
    body: null,
  } as unknown as Response;
}

function subscribe(fetchMock: typeof fetch) {
  const transport = createConnectTransport({
    baseUrl: "http://localhost",
    interceptors: [sessionInterceptor],
    fetch: fetchMock,
  });
  return createClient(NotificationService, transport).subscribeNotifications(
    create(SubscribeNotificationsRequestSchema, { parent: "workspaces/-" })
  );
}

async function collect(
  stream: AsyncIterable<SubscribeNotificationsResponse>
): Promise<SubscribeNotificationsResponse[]> {
  const messages: SubscribeNotificationsResponse[] = [];
  for await (const message of stream) {
    messages.push(message);
  }
  return messages;
}

async function failureOf(
  stream: AsyncIterable<SubscribeNotificationsResponse>
): Promise<unknown> {
  try {
    await collect(stream);
    return undefined;
  } catch (error) {
    return error;
  }
}

describe("the notification stream and an expired session", () => {
  afterEach(() => {
    setUnauthenticatedHandler(undefined);
    setSessionRefresher(undefined);
  });

  it("renews the session at the handshake, and the next reconnect carries it", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);
    const refresher = vi.fn().mockResolvedValue(undefined);
    setSessionRefresher(refresher);
    // The refused handshake, then the stream a fresh call opens once the cookie is renewed.
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(unauthorized())
      .mockResolvedValueOnce(streamed());

    const failure = await failureOf(subscribe(fetchMock));

    expect(refresher).toHaveBeenCalledTimes(1);
    expect(handler).not.toHaveBeenCalled();
    // What failed is the interceptor's replay, not the renewal. This pins connect-web's
    // one-shot request body: if that ever changes, this assertion is the signal, and the
    // comment above explains what the store relies on instead.
    expect(failure).toBeInstanceOf(ConnectError);
    expect((failure as ConnectError).code).toBe(Code.Unknown);

    // The connect loop reconnects with what the renewal installed, and that call works.
    const messages = await collect(subscribe(fetchMock));
    expect(messages).toHaveLength(1);
    expect(messages[0].event.case).toBe("notification");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("ends the session when the renewal is refused", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);
    setSessionRefresher(
      vi.fn().mockRejectedValue(new Error("no refresh token"))
    );
    const fetchMock = vi.fn().mockResolvedValue(unauthorized());

    const failure = await failureOf(subscribe(fetchMock));

    expect(failure).toBeInstanceOf(ConnectError);
    expect((failure as ConnectError).code).toBe(Code.Unauthenticated);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledTimes(1);
  });
});
