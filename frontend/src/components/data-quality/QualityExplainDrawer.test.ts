import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  ExplainQualityExecutionResponseSchema,
  QualityExecutionSchema,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityExplainDrawer from "./QualityExplainDrawer.vue";

const mocks = vi.hoisted(() => ({
  explainQualityExecution: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  explainQualityExecution: mocks.explainQualityExecution,
}));

function execution() {
  return create(QualityExecutionSchema, {
    name: "workspaces/ws/qualityExecutions/r1",
    outcome: QualityStatus.FAIL,
  });
}

function content(value: string) {
  return create(ExplainQualityExecutionResponseSchema, {
    payload: { case: "content", value },
  });
}

function progress(type: string) {
  return create(ExplainQualityExecutionResponseSchema, {
    payload: { case: "progress", value: { type, toolName: "read_result" } },
  });
}

function metadata() {
  return create(ExplainQualityExecutionResponseSchema, {
    payload: {
      case: "metadata",
      value: { provider: "openai", model: "gpt-test", fromCache: false },
    },
  });
}

function streamed(chunks: ReturnType<typeof content>[]) {
  return (async function* () {
    for (const chunk of chunks) {
      yield chunk;
    }
  })();
}

async function settle() {
  await flushPromises();
  await new Promise((resolve) => setTimeout(resolve, 0));
  await flushPromises();
}

describe("QualityExplainDrawer", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
  });

  it("streams progress and content and shows the final metadata", async () => {
    mocks.explainQualityExecution.mockReturnValue(
      streamed([
        progress("tool_start"),
        content("The null rate "),
        progress("tool_end"),
        content("rose to **4%**."),
        metadata(),
      ])
    );
    const wrapper = mount(QualityExplainDrawer, {
      attachTo: document.body,
      props: {
        open: true,
        execution: execution(),
        executionTitle: "Null rate on orders.id",
      },
      global: { plugins: [i18n] },
    });
    await settle();

    expect(mocks.explainQualityExecution).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "workspaces/ws/qualityExecutions/r1",
        language: "en-US",
        forceRegenerate: false,
      })
    );
    // The mock's second content frame lands after the first render; the
    // streaming panel must show content before the final frame arrives.
    await vi.waitFor(() => {
      expect(document.body.textContent).toContain("The null rate");
    });
    expect(document.body.textContent).toContain("openai / gpt-test");
    expect(
      document.body.querySelector('[aria-label="Explain with AI"]')
    ).toBeNull();
    wrapper.unmount();
  });

  it("regenerates with force_regenerate", async () => {
    mocks.explainQualityExecution.mockReturnValue(
      streamed([content("Cached answer"), metadata()])
    );
    const wrapper = mount(QualityExplainDrawer, {
      attachTo: document.body,
      props: { open: true, execution: execution() },
      global: { plugins: [i18n] },
    });
    await settle();
    await new Promise((r) => setTimeout(r, 500));
    await vi.waitFor(() => {
      expect(document.body.textContent).toContain("Cached answer");
      expect(document.body.textContent).toContain("openai / gpt-test");
    });

    const regenerate = [...document.body.querySelectorAll("button")].find(
      (button) =>
        button.textContent?.includes(
          i18n.global.t("dataQuality.llm.regenerate")
        )
    );
    regenerate?.click();
    await settle();

    expect(mocks.explainQualityExecution).toHaveBeenLastCalledWith(
      expect.objectContaining({ forceRegenerate: true })
    );
    wrapper.unmount();
  });

  it("maps a not-configured failure to friendly copy", async () => {
    const { ConnectError, Code } = await import("@connectrpc/connect");
    mocks.explainQualityExecution.mockImplementation(() => {
      throw new ConnectError(
        "LLM analysis is disabled",
        Code.FailedPrecondition
      );
    });
    const wrapper = mount(QualityExplainDrawer, {
      attachTo: document.body,
      props: { open: true, execution: execution() },
      global: { plugins: [i18n] },
    });
    await settle();

    expect(document.body.textContent).toContain(
      i18n.global.t("dataQuality.llm.errors.notConfigured")
    );
    wrapper.unmount();
  });
});
