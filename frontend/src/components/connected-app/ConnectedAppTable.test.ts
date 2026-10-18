import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  type ConnectedApp,
  ConnectedApp_State,
  ConnectedAppSchema,
} from "@/types/proto-es/v1/connected_app_service_pb";
import ConnectedAppTable from "./ConnectedAppTable.vue";

const now = new Date("2026-10-18T12:00:00Z");

function app(
  overrides: Partial<Omit<ConnectedApp, "$typeName" | "$unknown">> = {}
): ConnectedApp {
  return create(ConnectedAppSchema, {
    name: "connectedApps/7",
    clientId: "client-abc",
    clientName: "Claude Code",
    user: "users/11",
    userEmail: "ada@example.com",
    createTime: timestampFromDate(now),
    expireTime: timestampFromDate(new Date("2026-11-17T12:00:00Z")),
    state: ConnectedApp_State.ACTIVE,
    ...overrides,
  });
}

function mountTable(apps: ConnectedApp[], props: { showUser?: boolean } = {}) {
  return mount(ConnectedAppTable, {
    props: { apps, ...props },
    global: { plugins: [i18n] },
  });
}

const badges = (wrapper: ReturnType<typeof mountTable>) =>
  wrapper.findAll(".rounded-full");

describe("ConnectedAppTable", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.global.locale.value = "en-US";
  });

  // The registered name is self-reported; when it is missing the opaque client
  // id is the only honest label.
  it("falls back to the client id when the client name is empty", () => {
    const wrapper = mountTable([app({ clientName: "" })]);

    expect(wrapper.text()).toContain("client-abc");
    expect(wrapper.text()).not.toContain("Claude Code");
  });

  it("shows the client id under the name when both exist", () => {
    const wrapper = mountTable([app()]);

    expect(wrapper.text()).toContain("Claude Code");
    expect(wrapper.text()).toContain("client-abc");
  });

  it("renders the user column only for administrators", () => {
    const withUser = mountTable([app()], { showUser: true });
    expect(withUser.text()).toContain("ada@example.com");

    const withoutUser = mountTable([app()], { showUser: false });
    expect(withoutUser.text()).not.toContain("ada@example.com");
    expect(withoutUser.text()).not.toContain("users/11");
  });

  // Red is reserved for failures, so each state is a tone, not an error.
  it("maps every state to its label and badge variant", () => {
    const cases: Array<[ConnectedApp_State, string, string]> = [
      [ConnectedApp_State.ACTIVE, "Active", "bg-green-100"],
      [ConnectedApp_State.EXPIRED, "Expired", "bg-yellow-100"],
      [
        ConnectedApp_State.REAUTH_REQUIRED,
        "Reauthorization required",
        "bg-yellow-100",
      ],
      [ConnectedApp_State.REVOKED, "Revoked", "bg-secondary"],
      [ConnectedApp_State.INACTIVE, "Inactive", "text-foreground"],
      [ConnectedApp_State.STATE_UNSPECIFIED, "Unknown", "text-foreground"],
    ];

    for (const [state, label, variantClass] of cases) {
      const wrapper = mountTable([app({ state })]);
      expect(wrapper.text()).toContain(label);
      expect(badges(wrapper)[0].classes()).toContain(variantClass);
    }
  });

  it("offers revoke only while there is access left to cut", () => {
    for (const state of [
      ConnectedApp_State.REVOKED,
      ConnectedApp_State.EXPIRED,
      ConnectedApp_State.INACTIVE,
    ]) {
      const wrapper = mountTable([app({ state })]);
      expect(
        wrapper.findAll("button").some((button) => button.text() === "Revoke")
      ).toBe(false);
    }

    for (const state of [
      ConnectedApp_State.ACTIVE,
      ConnectedApp_State.REAUTH_REQUIRED,
    ]) {
      const wrapper = mountTable([app({ state })]);
      const button = wrapper
        .findAll("button")
        .find((candidate) => candidate.text() === "Revoke");
      expect(button, `state ${state} must be revokable`).toBeDefined();
    }
  });

  it("emits the row being revoked", async () => {
    const row = app();
    const wrapper = mountTable([row]);

    const button = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Revoke");
    await button?.trigger("click");

    expect(wrapper.emitted("revoke")?.[0]).toEqual([row]);
  });

  it("says a connection has not called a tool yet", () => {
    const wrapper = mountTable([app({ lastToolCallTime: undefined })]);
    expect(wrapper.text()).toContain("No tool call yet");
  });

  it("shows the last tool call as a relative time", () => {
    const wrapper = mountTable([
      app({
        lastToolCallTime: timestampFromDate(
          new Date(Date.now() - 2 * 60 * 60 * 1000)
        ),
      }),
    ]);

    expect(wrapper.text()).toContain("ago");
    expect(wrapper.text()).not.toContain("No tool call yet");
  });
});
