import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Input } from "@/components/ui/input";
import { i18n } from "@/locales";
import {
  CreateQualityAgentResponseSchema,
  QualityAgentSchema,
  RotateQualityAgentTokenResponseSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityAgentsCard from "./QualityAgentsCard.vue";

const mocks = vi.hoisted(() => ({
  listQualityAgents: vi.fn(),
  createQualityAgent: vi.fn(),
  rotateQualityAgentToken: vi.fn(),
  disableQualityAgent: vi.fn(),
}));

const notifyMock = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityAgents: mocks.listQualityAgents,
  createQualityAgent: mocks.createQualityAgent,
  rotateQualityAgentToken: mocks.rotateQualityAgentToken,
  disableQualityAgent: mocks.disableQualityAgent,
}));

vi.mock("@/lib/notify", () => ({ notify: notifyMock }));

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function agentFixture(name: string, enabled = true) {
  return create(QualityAgentSchema, {
    name: `workspaces/ws1/qualityAgents/${name}`,
    displayName: name,
    enabled,
  });
}

function mountCard(canUpdate = true) {
  return mount(QualityAgentsCard, {
    attachTo: document.body,
    props: { canUpdate },
    global: { plugins: [i18n] },
  });
}

function buttonsByText(text: string): HTMLElement[] {
  return [...document.body.querySelectorAll<HTMLElement>("button")].filter(
    (button) => button.textContent?.trim() === text
  );
}

function buttonByText(text: string): HTMLElement | undefined {
  return buttonsByText(text)[0];
}

/** A dialog's confirm reuses the page action's label; it is the later one. */
function dialogButtonByText(text: string): HTMLElement | undefined {
  const matches = [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].filter((button) => button.textContent?.trim() === text);
  return matches[matches.length - 1];
}

describe("QualityAgentsCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.listQualityAgents.mockResolvedValue([
      agentFixture("proposer"),
      agentFixture("retired", false),
    ]);
    mocks.createQualityAgent.mockResolvedValue(
      create(CreateQualityAgentResponseSchema, {
        agent: agentFixture("proposer"),
        token: "token-once",
      })
    );
    mocks.rotateQualityAgentToken.mockResolvedValue(
      create(RotateQualityAgentTokenResponseSchema, {
        agent: agentFixture("proposer"),
        token: "token-rotated",
      })
    );
    mocks.disableQualityAgent.mockResolvedValue(
      agentFixture("proposer", false)
    );
  });

  it("lists the agents and hides write actions from a reader", async () => {
    const wrapper = mountCard(false);
    await flushPromises();

    expect(wrapper.text()).toContain("proposer");
    expect(wrapper.text()).toContain(
      label("dataQualitySettings.agentsDisabled")
    );
    expect(
      buttonByText(label("dataQualitySettings.agentsCreate"))
    ).toBeUndefined();
    expect(
      buttonByText(label("dataQualitySettings.agentsDisable"))
    ).toBeUndefined();
    wrapper.unmount();
  });

  it("creates an agent and shows its token exactly once", async () => {
    const wrapper = mountCard();
    await flushPromises();

    buttonByText(label("dataQualitySettings.agentsCreate"))?.click();
    await flushPromises();
    wrapper.findComponent(Input).vm.$emit("update:modelValue", "proposer");
    await flushPromises();
    dialogButtonByText(label("dataQualitySettings.agentsCreate"))?.click();
    await flushPromises();

    expect(mocks.createQualityAgent).toHaveBeenCalledWith(
      expect.objectContaining({ displayName: "proposer" })
    );
    expect(document.body.textContent).toContain("token-once");
    expect(document.body.textContent).toContain(
      label("dataQualitySettings.agentsTokenTitle")
    );
    wrapper.unmount();
  });

  it("rotates a token and reports the new one", async () => {
    const wrapper = mountCard();
    await flushPromises();

    buttonByText(label("dataQualitySettings.agentsRotate"))?.click();
    await flushPromises();

    expect(mocks.rotateQualityAgentToken).toHaveBeenCalledWith(
      "workspaces/ws1/qualityAgents/proposer"
    );
    expect(document.body.textContent).toContain("token-rotated");
    wrapper.unmount();
  });

  it("requires confirmation before disabling an agent", async () => {
    const wrapper = mountCard();
    await flushPromises();

    buttonByText(label("dataQualitySettings.agentsDisable"))?.click();
    await flushPromises();
    expect(mocks.disableQualityAgent).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      label("dataQualitySettings.agentsDisableTitle")
    );
    const confirm = [
      ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
    ].find(
      (button) =>
        button.textContent?.trim() ===
        label("dataQualitySettings.agentsDisable")
    );
    confirm?.click();
    await flushPromises();

    expect(mocks.disableQualityAgent).toHaveBeenCalledWith(
      "workspaces/ws1/qualityAgents/proposer"
    );
    wrapper.unmount();
  });

  it("reports a failed load instead of an empty list", async () => {
    mocks.listQualityAgents.mockRejectedValueOnce(
      new ConnectError("boom", Code.Internal)
    );
    const wrapper = mountCard();
    await flushPromises();

    expect(document.body.textContent).not.toContain(
      label("dataQualitySettings.agentsEmpty")
    );
    expect(document.body.textContent).toContain(
      "The server hit an internal error."
    );
    wrapper.unmount();
  });
});
