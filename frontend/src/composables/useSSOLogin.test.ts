import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  getWorkspaceProfileSetting: vi.fn(),
  createSSOState: vi.fn(),
  beginSSOLogin: vi.fn(),
}));

vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
}));
vi.mock("@/api/auth", () => ({ createSSOState: mocks.createSSOState }));
vi.mock("@/lib/oauth", () => ({ beginSSOLogin: mocks.beginSSOLogin }));

import { useSSOLogin } from "@/composables/useSSOLogin";
import {
  IdentityProviderSchema,
  IdentityProviderType,
} from "@/types/proto-es/v1/identity_provider_service_pb";

const provider = create(IdentityProviderSchema, {
  name: "idps/acme",
  title: "Acme",
  type: IdentityProviderType.OAUTH2,
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getWorkspaceProfileSetting.mockResolvedValue({
    externalUrl: "https://metaxis.example.com",
  });
  mocks.createSSOState.mockResolvedValue("nonce");
  mocks.beginSSOLogin.mockResolvedValue(true);
});

describe("useSSOLogin", () => {
  it("issues the nonce and starts the flow against the workspace external URL", async () => {
    const { start, isStarting } = useSSOLogin();

    await expect(start(provider, { redirect: "/instances" })).resolves.toBe(
      true
    );

    expect(mocks.beginSSOLogin).toHaveBeenCalledWith(provider, {
      serverState: "nonce",
      mode: undefined,
      redirect: "/instances",
      externalUrl: "https://metaxis.example.com",
    });
    expect(isStarting.value).toBe(false);
  });

  it("passes the test mode through", async () => {
    const { start } = useSSOLogin();

    await start(provider, { mode: "test" });

    expect(mocks.beginSSOLogin).toHaveBeenCalledWith(
      provider,
      expect.objectContaining({ mode: "test" })
    );
  });

  it("reports a provider the flow could not be started for", async () => {
    mocks.beginSSOLogin.mockResolvedValue(false);
    const { start } = useSSOLogin();

    await expect(start(provider)).resolves.toBe(false);
  });

  it("ignores a second click while the first start is in flight", async () => {
    let release: (started: boolean) => void = () => {};
    mocks.beginSSOLogin.mockReturnValue(
      new Promise<boolean>((resolve) => {
        release = resolve;
      })
    );
    const { start, isStarting } = useSSOLogin();

    const first = start(provider);
    expect(isStarting.value).toBe(true);
    await expect(start(provider)).resolves.toBe(false);
    expect(mocks.getWorkspaceProfileSetting).toHaveBeenCalledTimes(1);

    release(true);
    await expect(first).resolves.toBe(true);
  });

  it("leaves the button usable again after a transport failure", async () => {
    mocks.createSSOState.mockRejectedValue(new Error("unavailable"));
    const { start, isStarting } = useSSOLogin();

    await expect(start(provider)).rejects.toThrow("unavailable");
    expect(isStarting.value).toBe(false);
  });
});
