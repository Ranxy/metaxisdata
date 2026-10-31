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

/**
 * A stream that delivers one frame, waits until its request is aborted, and
 * then delivers a final frame — the ordering a real stream can produce when a
 * server frame is already in flight.
 */
function staleFrameAfterAbort(signal: AbortSignal) {
  return (async function* () {
    yield content("Old answer");
    await new Promise<void>((resolve) => {
      if (signal.aborted) {
        resolve();
        return;
      }
      signal.addEventListener("abort", () => resolve(), { once: true });
    });
    yield content("Late frame");
  })();
}

/** A stream that delivers its chunks and then never finishes on its own. */
function hanging(chunks: ReturnType<typeof content>[]) {
  return (async function* () {
    for (const chunk of chunks) {
      yield chunk;
    }
    await new Promise(() => {});
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

  it("aborts the previous stream when the drawer switches execution", async () => {
    const first = create(QualityExecutionSchema, {
      name: "workspaces/ws/qualityExecutions/r1",
      outcome: QualityStatus.FAIL,
    });
    const second = create(QualityExecutionSchema, {
      name: "workspaces/ws/qualityExecutions/r2",
      outcome: QualityStatus.FAIL,
    });
    mocks.explainQualityExecution
      .mockReturnValueOnce(hanging([content("Old answer")]))
      .mockReturnValueOnce(streamed([content("New answer"), metadata()]));

    const wrapper = mount(QualityExplainDrawer, {
      attachTo: document.body,
      props: { open: true, execution: first },
      global: { plugins: [i18n] },
    });
    await settle();
    const firstSignal = mocks.explainQualityExecution.mock.calls[0]?.[0]
      .signal as AbortSignal;
    expect(firstSignal.aborted).toBe(false);

    await wrapper.setProps({ execution: second });
    await settle();

    // The superseded stream is aborted, and its buffered frame never lands in
    // the document the new execution owns.
    expect(firstSignal.aborted).toBe(true);
    expect(document.body.textContent).toContain("New answer");
    expect(document.body.textContent).not.toContain("Old answer");
    wrapper.unmount();
  });

  it("drops a frame the aborted stream delivers after the switch", async () => {
    const first = create(QualityExecutionSchema, {
      name: "workspaces/ws/qualityExecutions/r1",
      outcome: QualityStatus.FAIL,
    });
    const second = create(QualityExecutionSchema, {
      name: "workspaces/ws/qualityExecutions/r2",
      outcome: QualityStatus.FAIL,
    });
    mocks.explainQualityExecution
      .mockImplementationOnce((options: { signal: AbortSignal }) =>
        staleFrameAfterAbort(options.signal)
      )
      .mockReturnValueOnce(streamed([content("New answer"), metadata()]));

    const wrapper = mount(QualityExplainDrawer, {
      attachTo: document.body,
      props: { open: true, execution: first },
      global: { plugins: [i18n] },
    });
    await settle();

    await wrapper.setProps({ execution: second });
    await settle();

    expect(document.body.textContent).toContain("New answer");
    expect(document.body.textContent).not.toContain("Late frame");
    wrapper.unmount();
  });

  it("aborts the stream when the drawer closes", async () => {
    mocks.explainQualityExecution.mockReturnValue(
      hanging([content("Still generating")])
    );
    const wrapper = mount(QualityExplainDrawer, {
      attachTo: document.body,
      props: { open: true, execution: execution() },
      global: { plugins: [i18n] },
    });
    await settle();
    const signal = mocks.explainQualityExecution.mock.calls[0]?.[0]
      .signal as AbortSignal;

    await wrapper.setProps({ open: false });

    expect(signal.aborted).toBe(true);
    wrapper.unmount();
  });
});
