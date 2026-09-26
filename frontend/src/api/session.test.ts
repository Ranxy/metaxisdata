import { Code, ConnectError, type UnaryRequest } from "@connectrpc/connect";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthService } from "@/types/proto-es/v1/auth_service_pb";
import { sessionInterceptor, setUnauthenticatedHandler } from "./session";

function request(service: string): UnaryRequest {
  return {
    service: { typeName: service },
    method: { name: "Test" },
  } as unknown as UnaryRequest;
}

function failingNext(error: unknown) {
  return vi.fn().mockRejectedValue(error);
}

describe("sessionInterceptor", () => {
  afterEach(() => {
    setUnauthenticatedHandler(undefined);
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

    const error = new ConnectError("no cookie", Code.Unauthenticated);
    const next = failingNext(error);

    await expect(
      sessionInterceptor(next)(request("metaxisdata.v1.InstanceService"))
    ).rejects.toBe(error);
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("leaves a failed login to the login form", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);

    await expect(
      sessionInterceptor(
        failingNext(new ConnectError("bad pw", Code.Unauthenticated))
      )(request("metaxisdata.v1.AuthService"))
    ).rejects.toThrow(ConnectError);
    expect(handler).not.toHaveBeenCalled();
  });

  it("ignores other Connect codes", async () => {
    const handler = vi.fn();
    setUnauthenticatedHandler(handler);

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
      sessionInterceptor(
        failingNext(new ConnectError("no cookie", Code.Unauthenticated))
      )(request("metaxisdata.v1.InstanceService"))
    ).rejects.toThrow(ConnectError);
  });
});
