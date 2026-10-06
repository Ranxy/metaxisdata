import { Code, ConnectError, type UnaryRequest } from "@connectrpc/connect";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthService } from "@/types/proto-es/v1/auth_service_pb";
import {
  sessionInterceptor,
  setSessionRefresher,
  setUnauthenticatedHandler,
} from "./session";

function request(service: string): UnaryRequest {
  return {
    service: { typeName: service },
    method: { name: "Test" },
  } as unknown as UnaryRequest;
}

function failingNext(error: unknown) {
  return vi.fn().mockRejectedValue(error);
}

function expired() {
  return new ConnectError("no cookie", Code.Unauthenticated);
}

describe("sessionInterceptor", () => {
  afterEach(() => {
    setUnauthenticatedHandler(undefined);
    setSessionRefresher(undefined);
  });

  it("exempts the service descriptor the app really uses for auth", () => {
    // The exemption compares the descriptor's typeName; a regenerated proto that
    // renamed the service would silently start logging users out on a bad
    // password without this.
    expect(AuthService.typeName).toBe("metaxisdata.v1.AuthService");
  });

  it("reports an expired session once the cookie is gone", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);

    const error = expired();
    const next = failingNext(error);

    await expect(
      sessionInterceptor(next)(request("metaxisdata.v1.InstanceService"))
    ).rejects.toBe(error);
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("renews the session and replays the request", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);
    const refresher = vi.fn().mockResolvedValue(undefined);
    setSessionRefresher(refresher);

    const response = { message: {} };
    const next = vi
      .fn()
      .mockRejectedValueOnce(expired())
      .mockResolvedValueOnce(response);

    await expect(
      sessionInterceptor(next)(request("metaxisdata.v1.InstanceService"))
    ).resolves.toBe(response);
    expect(refresher).toHaveBeenCalledTimes(1);
    expect(next).toHaveBeenCalledTimes(2);
    expect(handler).not.toHaveBeenCalled();
  });

  it("ends the session when the renewal is refused", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);
    const refresher = vi
      .fn()
      .mockRejectedValue(new ConnectError("invalid", Code.Unauthenticated));
    setSessionRefresher(refresher);

    const error = expired();
    const next = failingNext(error);

    await expect(
      sessionInterceptor(next)(request("metaxisdata.v1.InstanceService"))
    ).rejects.toBe(error);
    expect(refresher).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledTimes(1);
    expect(next).toHaveBeenCalledTimes(1);
  });

  it("retries at most once when the replay is refused too", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);
    const refresher = vi.fn().mockResolvedValue(undefined);
    setSessionRefresher(refresher);

    const error = expired();
    const next = failingNext(error);

    await expect(
      sessionInterceptor(next)(request("metaxisdata.v1.InstanceService"))
    ).rejects.toBe(error);
    expect(next).toHaveBeenCalledTimes(2);
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("shares one renewal between concurrent requests", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);

    // A refresh token is single-use, so two rotations racing would leave the
    // loser holding a consumed cookie and log a valid session out.
    let release: () => void = () => {};
    const refresher = vi.fn().mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        })
    );
    setSessionRefresher(refresher);

    const first = vi
      .fn()
      .mockRejectedValueOnce(expired())
      .mockResolvedValueOnce({ message: {} });
    const second = vi
      .fn()
      .mockRejectedValueOnce(expired())
      .mockResolvedValueOnce({ message: {} });

    const firstRequest = sessionInterceptor(first)(
      request("metaxisdata.v1.InstanceService")
    );
    const secondRequest = sessionInterceptor(second)(
      request("metaxisdata.v1.InstanceService")
    );

    await vi.waitFor(() => expect(refresher).toHaveBeenCalledTimes(1));
    release();

    await expect(
      Promise.all([firstRequest, secondRequest])
    ).resolves.toHaveLength(2);
    expect(refresher).toHaveBeenCalledTimes(1);
    expect(handler).not.toHaveBeenCalled();
  });

  it("leaves a failed login to the login form", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);
    const refresher = vi.fn().mockResolvedValue(undefined);
    setSessionRefresher(refresher);

    await expect(
      sessionInterceptor(
        failingNext(new ConnectError("bad pw", Code.Unauthenticated))
      )(request("metaxisdata.v1.AuthService"))
    ).rejects.toThrow(ConnectError);
    expect(handler).not.toHaveBeenCalled();
    expect(refresher).not.toHaveBeenCalled();
  });

  it("ignores other Connect codes", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);
    setSessionRefresher(vi.fn().mockResolvedValue(undefined));

    await expect(
      sessionInterceptor(failingNext(new ConnectError("boom", Code.Internal)))(
        request("metaxisdata.v1.InstanceService")
      )
    ).rejects.toThrow(ConnectError);
    expect(handler).not.toHaveBeenCalled();
  });

  it("passes a successful response through untouched", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);
    const response = { message: {} };
    const next = vi.fn().mockResolvedValue(response);

    await expect(
      sessionInterceptor(next)(request("metaxisdata.v1.InstanceService"))
    ).resolves.toBe(response);
    expect(handler).not.toHaveBeenCalled();
  });

  it("works before a handler is registered", async () => {
    setUnauthenticatedHandler(undefined);

    await expect(
      sessionInterceptor(failingNext(expired()))(
        request("metaxisdata.v1.InstanceService")
      )
    ).rejects.toThrow(ConnectError);
  });
});
