import { beforeEach, describe, expect, it, vi } from "vitest";
import { DeviceLoginState } from "@/types/proto-es/v1/auth_service_pb";
import {
  approveDeviceLogin,
  deviceLoginName,
  getDeviceLogin,
  normalizeUserCode,
} from "./device-login";

const { getDeviceLoginMock, approveDeviceLoginMock } = vi.hoisted(() => ({
  getDeviceLoginMock: vi.fn(),
  approveDeviceLoginMock: vi.fn(),
}));

vi.mock("./client", () => ({
  authClient: {
    getDeviceLogin: getDeviceLoginMock,
    approveDeviceLogin: approveDeviceLoginMock,
  },
}));

beforeEach(() => {
  getDeviceLoginMock.mockReset();
  approveDeviceLoginMock.mockReset();
});

describe("normalizeUserCode", () => {
  it("keeps what a person would type and drops the separators", () => {
    expect(normalizeUserCode("7q2x-9m4k")).toBe("7Q2X9M4K");
    expect(normalizeUserCode(" 7Q2X 9M4K ")).toBe("7Q2X9M4K");
    // Crockford confusables survive: the server decodes them to a digit.
    expect(normalizeUserCode("ooii-lioo")).toBe("OOIILIOO");
  });
});

describe("deviceLoginName", () => {
  it("addresses the request by its normalized code", () => {
    expect(deviceLoginName("7q2x-9m4k")).toBe("deviceLogins/7Q2X9M4K");
  });
});

describe("getDeviceLogin", () => {
  it("reads the request the code refers to", async () => {
    getDeviceLoginMock.mockResolvedValueOnce({
      name: "deviceLogins/7Q2X9M4K",
      state: DeviceLoginState.PENDING,
    });

    const login = await getDeviceLogin("7q2x-9m4k");

    expect(login.state).toBe(DeviceLoginState.PENDING);
    // The client receives a message, so the request is inspected by field.
    expect(getDeviceLoginMock.mock.calls[0][0].name).toBe(
      "deviceLogins/7Q2X9M4K"
    );
  });
});

describe("approveDeviceLogin", () => {
  it("sends the decision for the normalized code", async () => {
    approveDeviceLoginMock.mockResolvedValueOnce({});

    await approveDeviceLogin("7q2x-9m4k", false);

    const request = approveDeviceLoginMock.mock.calls[0][0];
    expect(request.name).toBe("deviceLogins/7Q2X9M4K");
    expect(request.approve).toBe(false);
  });
});
