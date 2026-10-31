import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { ExplainSQLResponseSchema } from "@/types/proto-es/v1/explain_sql_service_pb";
import ExplainSQLPage from "./ExplainSQLPage.vue";

const mocks = vi.hoisted(() => ({
  explainSQL: vi.fn(),
}));

vi.mock("@/api/explain", () => ({ explainSQL: mocks.explainSQL }));
vi.mock("@/api/database", () => ({
  searchMetadata: vi.fn(async () => ({ results: [] })),
  listMetadata: vi.fn(async () => ({})),
  getSchemaString: vi.fn(async () => ({ schema: "" })),
}));
vi.mock("@/api/llm", () => ({
  listProfiles: vi.fn(async () => ({ profiles: [] })),
}));
vi.mock("@/api/setting", () => ({
  getWorkspaceProfileSetting: vi.fn(async () => ({
    allowedLlmProviderProfiles: [],
  })),
}));
vi.mock("@/store/modules/instance", () => ({
  useInstanceStore: () => ({
    ensureLoaded: async () => {},
    active: [],
  }),
}));

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
});

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
});
