import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { Badge } from "@/components/ui/badge";
import { i18n } from "@/locales";
import {
  type ConnectedApp,
  ConnectedApp_Kind,
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

function mountTable(
  apps: ConnectedApp[],
  props: {
    showUser?: boolean;
    canRevokeOthers?: boolean;
    callerUser?: string;
  } = {}
) {
  return mount(ConnectedAppTable, {
    props: { apps, callerUser: "users/11", ...props },
    global: { plugins: [i18n] },
  });
}

describe("ConnectedAppTable", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.global.locale.value = "en-US";
  });

  // The registered name is self-reported; when it is missing the opaque client
  // id is the only honest label.
  it("shows the client name and id, falling back to the id alone", () => {
    const named = mountTable([app()]);
    expect(named.text()).toContain("Claude Code");
    expect(named.text()).toContain("client-abc");

    const unnamed = mountTable([app({ clientName: "" })]);
    expect(unnamed.text()).toContain("client-abc");
    expect(unnamed.text()).not.toContain("Claude Code");
  });

  it("renders the user column only for administrators", () => {
    const withUser = mountTable([app()], { showUser: true });
    expect(withUser.text()).toContain("ada@example.com");

    const withoutUser = mountTable([app()], { showUser: false });
    expect(withoutUser.text()).not.toContain("ada@example.com");
    expect(withoutUser.text()).not.toContain("users/11");
  });

  // Red is reserved for failures, so each state is a tone, not an error. The
  // assertion is the badge's variant, not the Tailwind class it resolves to: a
  // palette change must not fail a test that is about which tone a state gets.
  it("maps every state to its label and tone", () => {
    const cases: Array<[ConnectedApp_State, string, string]> = [
      [ConnectedApp_State.ACTIVE, "Active", "success"],
      [ConnectedApp_State.EXPIRED, "Expired", "warning"],
      [
        ConnectedApp_State.REAUTH_REQUIRED,
        "Reauthorization required",
        "warning",
      ],
      [ConnectedApp_State.REVOKED, "Revoked", "secondary"],
      [ConnectedApp_State.INACTIVE, "Inactive", "outline"],
      [ConnectedApp_State.STATE_UNSPECIFIED, "Unknown", "outline"],
    ];

    for (const [state, label, variant] of cases) {
      const wrapper = mountTable([app({ state })]);
      expect(wrapper.text(), `state ${state}`).toContain(label);
      expect(
        wrapper.findComponent(Badge).props("variant"),
        `state ${state}`
      ).toBe(variant);
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

  // The server refuses another user's row with a uniform NotFound, so a button
  // that could only fail would mislead; ownership plus the revoke permission is
  // what makes it appear.
  it("hides revoke for another user's row unless the caller may revoke others", () => {
    const other = app({ user: "users/12" });

    const limited = mountTable([other]);
    expect(
      limited.findAll("button").some((button) => button.text() === "Revoke")
    ).toBe(false);

    const allowed = mountTable([other], { canRevokeOthers: true });
    expect(
      allowed.findAll("button").some((button) => button.text() === "Revoke")
    ).toBe(true);
  });

  // A CLI login is one device: every session shares the built-in client name, so
  // the version and source address are what let a user tell their own apart.
  it("names the device behind a CLI session", () => {
    const wrapper = mountTable([
      app({
        clientId: "mxd",
        clientName: "Metaxisdata CLI",
        kind: ConnectedApp_Kind.CLI,
        clientVersion: "0.1.0",
        requestIp: "192.168.1.20",
      }),
    ]);

    expect(wrapper.text()).toContain("Metaxisdata CLI");
    expect(wrapper.text()).toContain("CLI · v0.1.0 · 192.168.1.20");
  });

  it("says a connection has not called a tool yet", () => {
    const wrapper = mountTable([app({ lastToolCallTime: undefined })]);
    expect(wrapper.text()).toContain("No tool call yet");
  });

  // A CLI session is used through API calls, not tool calls, so the column is
  // blank rather than claiming it has never called a tool.
  it("leaves the tool-call column blank for a CLI session", () => {
    const wrapper = mountTable([
      app({ kind: ConnectedApp_Kind.CLI, lastToolCallTime: undefined }),
    ]);
    expect(wrapper.text()).not.toContain("No tool call yet");
    expect(wrapper.text()).toContain("—");
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
