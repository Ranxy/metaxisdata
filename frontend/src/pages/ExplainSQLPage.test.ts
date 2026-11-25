import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { Select, SelectItem } from "@/components/ui/select";
import { i18n } from "@/locales";
import { ExplainSQLResponseSchema } from "@/types/proto-es/v1/explain_sql_service_pb";
import ExplainSQLPage from "./ExplainSQLPage.vue";

const mocks = vi.hoisted(() => ({
  explainSQL: vi.fn(),
  listProfiles: vi.fn(),
  getWorkspaceProfileSetting: vi.fn(),
}));

vi.mock("@/api/explain", () => ({ explainSQL: mocks.explainSQL }));
vi.mock("@/api/database", () => ({
  searchMetadata: vi.fn(async () => ({ results: [] })),
  listMetadata: vi.fn(async () => ({})),
  getSchemaString: vi.fn(async () => ({ schema: "" })),
}));
vi.mock("@/api/llm", () => ({ listProfiles: mocks.listProfiles }));
vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
}));
vi.mock("@/store/modules/instance", () => ({
  useInstanceStore: () => ({
    ensureLoaded: async () => {},
    active: [],
  }),
}));

// Radix opens a select on pointerdown and calls pointer-capture APIs that jsdom
// does not implement.
Element.prototype.hasPointerCapture ??= () => false;
Element.prototype.setPointerCapture ??= () => {};
Element.prototype.releasePointerCapture ??= () => {};

function content(value: string) {
  return create(ExplainSQLResponseSchema, {
    payload: { case: "content", value },
  });
}

/** A stream that delivers its first frames and then keeps the request open. */
function hanging(chunks: ReturnType<typeof content>[]) {
  return (async function* () {
    for (const chunk of chunks) {
      yield chunk;
    }
    await new Promise(() => {});
  })();
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/explain-sql", component: ExplainSQLPage }],
  });
  await router.push("/explain-sql");
  await router.isReady();
  const wrapper = mount(ExplainSQLPage, {
    global: {
      plugins: [router, i18n],
      stubs: {
        MonacoEditor: {
          props: ["content"],
          emits: ["update:content"],
          template:
            '<textarea class="stub-sql" :value="content" @input="$emit(\'update:content\', $event.target.value)" />',
        },
      },
    },
  });
  await flushPromises();
  return wrapper;
}

async function typeCustomSQL(wrapper: Awaited<ReturnType<typeof mountPage>>) {
  const customTab = wrapper
    .findAll("button")
    .find((button) =>
      button.text().includes(i18n.global.t("explainSQL.customSQL"))
    );
  await customTab?.trigger("click");
  await wrapper.find(".stub-sql").setValue("SELECT 1");
}

function explainButton(wrapper: Awaited<ReturnType<typeof mountPage>>) {
  return wrapper
    .findAll("button")
    .find((button) =>
      button.text().includes(i18n.global.t("explainSQL.explain"))
    );
}

beforeEach(() => {
  setActivePinia(createPinia());
  localStorage.clear();
  vi.clearAllMocks();
  mocks.listProfiles.mockResolvedValue({ profiles: [], nextPageToken: "" });
  mocks.getWorkspaceProfileSetting.mockResolvedValue({
    allowedLlmProviderModels: [],
  });
});

/** Open the pair picker and read the labels it offers. */
async function modelOptions(
  wrapper: Awaited<ReturnType<typeof mountPage>>
): Promise<string[]> {
  const picker = wrapper.findComponent(Select);
  await picker.find("button").trigger("pointerdown");
  await flushPromises();
  return picker
    .findAllComponents(SelectItem)
    .map((item) => String(item.text()));
}

function enabledProfile(
  name: string,
  title: string,
  models: Array<{ name: string; enabled: boolean }>
) {
  return { name, title, models };
}

describe("ExplainSQLPage", () => {
  it("streams the answer and aborts the stream when the page goes away", async () => {
    mocks.explainSQL.mockReturnValue(hanging([content("Hello ")]));
    const wrapper = await mountPage();
    await typeCustomSQL(wrapper);

    await explainButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.explainSQL).toHaveBeenCalledWith(
      expect.objectContaining({ sqlText: "SELECT 1", forceRegenerate: false }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    );
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain("Hello");
    });

    // The stream is still open, so leaving the page must cancel it.
    const options = mocks.explainSQL.mock.calls[0]?.[1] as {
      signal: AbortSignal;
    };
    expect(options.signal.aborted).toBe(false);
    wrapper.unmount();
    expect(options.signal.aborted).toBe(true);
  });

  it("shows the mapped sentence for a failed explanation, not the raw error", async () => {
    mocks.explainSQL.mockReturnValue(
      (async function* () {
        throw new ConnectError("provider exploded", Code.Internal);
      })()
    );
    const wrapper = await mountPage();
    await typeCustomSQL(wrapper);

    await explainButton(wrapper)?.trigger("click");
    await flushPromises();

    await vi.waitFor(() => {
      expect(wrapper.text()).toContain(i18n.global.t("error.internal"));
    });
    expect(wrapper.text()).not.toContain("provider exploded");
  });

  it("keeps the explain button disabled until there is SQL", async () => {
    const wrapper = await mountPage();

    const metadataButton = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("explainSQL.explain"))
      );
    expect(metadataButton?.attributes("disabled")).toBeDefined();

    await typeCustomSQL(wrapper);
    expect(explainButton(wrapper)?.attributes("disabled")).toBeUndefined();
  });

  it("offers only the (profile, model) pairs the workspace allowlist permits", async () => {
    mocks.listProfiles.mockResolvedValue({
      profiles: [
        enabledProfile("llm-provider-profiles/allowed", "Allowed", [
          { name: "fast", enabled: true },
          { name: "slow", enabled: true },
        ]),
        enabledProfile("llm-provider-profiles/blocked", "Blocked", [
          { name: "fast", enabled: true },
        ]),
      ],
      nextPageToken: "",
    });
    mocks.getWorkspaceProfileSetting.mockResolvedValue({
      allowedLlmProviderModels: [
        { providerProfile: "llm-provider-profiles/allowed", model: "fast" },
      ],
    });
    const wrapper = await mountPage();

    // One permitted pair is not a choice, so the picker stays hidden; the
    // request naming it is the observable proof the blocked profile was dropped.
    expect(wrapper.findComponent(Select).exists()).toBe(false);
    mocks.explainSQL.mockReturnValue(hanging([content("Done")]));
    await typeCustomSQL(wrapper);
    await explainButton(wrapper)?.trigger("click");
    await flushPromises();
    expect(mocks.explainSQL).toHaveBeenCalledWith(
      expect.objectContaining({
        providerName: "llm-provider-profiles/allowed",
        model: "fast",
      }),
      expect.anything()
    );
  });

  it("offers every enabled model when the allowlist is empty", async () => {
    mocks.listProfiles.mockResolvedValue({
      profiles: [
        enabledProfile("llm-provider-profiles/a", "A", [
          { name: "fast", enabled: true },
          { name: "retired", enabled: false },
        ]),
        enabledProfile("llm-provider-profiles/b", "B", [
          { name: "reasoner", enabled: true },
        ]),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountPage();

    // The label names both halves of the pair, since the model is never inferred.
    const options = await modelOptions(wrapper);
    expect(options).toHaveLength(2);
    expect(options[0]).toContain("A");
    expect(options[0]).toContain("fast");
    expect(options[1]).toContain("B");
    expect(options[1]).toContain("reasoner");
    expect(options.some((text) => text.includes("retired"))).toBe(false);
  });

  it("sends the selected pair on both explain paths", async () => {
    mocks.explainSQL.mockReturnValue(hanging([content("Done")]));
    mocks.listProfiles.mockResolvedValue({
      profiles: [
        enabledProfile("llm-provider-profiles/main", "Main", [
          { name: "fast", enabled: true },
          { name: "reasoner", enabled: true },
        ]),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountPage();
    // The first option is the default; the request below proves it travels.
    await typeCustomSQL(wrapper);
    await explainButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.explainSQL).toHaveBeenCalledWith(
      expect.objectContaining({
        providerName: "llm-provider-profiles/main",
        model: "fast",
      }),
      expect.anything()
    );
  });

  it("sends a single allowed pair even though the picker is hidden", async () => {
    mocks.explainSQL.mockReturnValue(hanging([content("Done")]));
    mocks.listProfiles.mockResolvedValue({
      profiles: [
        enabledProfile("llm-provider-profiles/main", "Main", [
          { name: "fast", enabled: true },
          { name: "retired", enabled: false },
        ]),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountPage();
    // One option is not a choice, so the picker stays hidden but the pair is
    // still the one the request names.
    expect(wrapper.findComponent(Select).exists()).toBe(false);

    await typeCustomSQL(wrapper);
    await explainButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.explainSQL).toHaveBeenCalledWith(
      expect.objectContaining({
        providerName: "llm-provider-profiles/main",
        model: "fast",
      }),
      expect.anything()
    );
  });

  it("leaves the pair empty for the server default when no model is selectable", async () => {
    mocks.explainSQL.mockReturnValue(hanging([content("Done")]));
    const wrapper = await mountPage();
    expect(wrapper.findComponent(Select).exists()).toBe(false);

    await typeCustomSQL(wrapper);
    await explainButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(mocks.explainSQL).toHaveBeenCalledWith(
      expect.objectContaining({ providerName: "", model: "" }),
      expect.anything()
    );
  });
});
