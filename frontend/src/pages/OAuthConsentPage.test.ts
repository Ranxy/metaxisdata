import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import OAuthConsentPage from "./OAuthConsentPage.vue";

const mocks = vi.hoisted(() => ({
  getOAuthAuthorizationRequest: vi.fn(),
  approveOAuthAuthorizationRequest: vi.fn(),
}));

vi.mock("@/api/oauth", () => ({
  getOAuthAuthorizationRequest: mocks.getOAuthAuthorizationRequest,
  approveOAuthAuthorizationRequest: mocks.approveOAuthAuthorizationRequest,
}));

const pendingRequest = {
  name: "oauthAuthorizationRequests/req-1",
  clientName: "Claude Code",
  redirectUri: "http://127.0.0.1:51004/callback",
  resource: "https://mx.example.com/mcp",
  scopes: ["metaxisdata.mcp.read"],
  requestIp: "203.0.113.7",
};

// jsdom's location is not writable field by field, so the whole object is
// replaced for the test and put back afterwards.
const originalLocation = window.location;
let assign: ReturnType<typeof vi.fn>;

beforeEach(() => {
  setActivePinia(createPinia());
  assign = vi.fn();
  Object.defineProperty(window, "location", {
    configurable: true,
    writable: true,
    value: { ...originalLocation, assign },
  });
  mocks.getOAuthAuthorizationRequest
    .mockReset()
    .mockResolvedValue(pendingRequest);
  mocks.approveOAuthAuthorizationRequest
    .mockReset()
    .mockResolvedValue(undefined);
});

afterEach(() => {
  Object.defineProperty(window, "location", {
    configurable: true,
    writable: true,
    value: originalLocation,
  });
});

async function mountPage(
  query: Record<string, string> = { request_id: "req-1" }
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/oauth/consent",
        name: "OAuthConsent",
        component: OAuthConsentPage,
      },
    ],
  });
  await router.push({ path: "/oauth/consent", query });
  await router.isReady();

  const wrapper = mount(OAuthConsentPage, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return wrapper;
}

async function clickButton(
  wrapper: Awaited<ReturnType<typeof mountPage>>,
  label: string
) {
  const button = wrapper
    .findAll("button")
    .find((candidate) => candidate.text().includes(label));
  expect(button, `the ${label} button must exist`).toBeDefined();
  await button?.trigger("click");
  await flushPromises();
}

describe("OAuthConsentPage", () => {
  it("approves nothing until the user decides", async () => {
    await mountPage();

    // Loading the page reads the request and nothing else: a link that approved
    // on open would let anyone who can reach it grant access.
    expect(mocks.getOAuthAuthorizationRequest).toHaveBeenCalledWith("req-1");
    expect(mocks.approveOAuthAuthorizationRequest).not.toHaveBeenCalled();
    expect(assign).not.toHaveBeenCalled();
  });

  it("does not call the API when the link carries no request id", async () => {
    await mountPage({});

    expect(mocks.getOAuthAuthorizationRequest).not.toHaveBeenCalled();
    expect(mocks.approveOAuthAuthorizationRequest).not.toHaveBeenCalled();
  });

  it("hands the browser to the completion endpoint on approval", async () => {
    const wrapper = await mountPage();
    await clickButton(wrapper, "Approve");

    expect(mocks.approveOAuthAuthorizationRequest).toHaveBeenCalledWith(
      "req-1",
      true
    );
    expect(assign).toHaveBeenCalledWith(
      "/oauth/authorize/complete?request_id=req-1"
    );
  });

  it("hands the browser to the completion endpoint on denial too", async () => {
    const wrapper = await mountPage();
    await clickButton(wrapper, "Deny");

    // The client is waiting for a callback; stopping at this page would leave it
    // waiting forever. The server answers this navigation with access_denied.
    expect(mocks.approveOAuthAuthorizationRequest).toHaveBeenCalledWith(
      "req-1",
      false
    );
    expect(assign).toHaveBeenCalledWith(
      "/oauth/authorize/complete?request_id=req-1"
    );
  });
});
