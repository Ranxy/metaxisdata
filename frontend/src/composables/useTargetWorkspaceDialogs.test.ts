import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { computed, defineComponent, nextTick, ref } from "vue";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/locales";
import {
  QualityRuleSchema,
  QualitySuggestionSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { useTargetWorkspaceDialogs } from "./useTargetWorkspaceDialogs";

const notifyInfo = vi.hoisted(() => vi.fn());
vi.mock("@/lib/notify", () => ({
  notify: {
    info: notifyInfo,
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
  },
}));

function rule(name: string) {
  return create(QualityRuleSchema, { name });
}

async function mountDialogs(
  initialQuery: Record<string, string> = {},
  options: {
    canCreate?: boolean;
    initialRules?: string[];
    initialLoading?: boolean;
  } = {}
) {
  const router: Router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/", component: { template: "<div />" } }],
  });
  await router.push({ path: "/", query: initialQuery });
  await router.isReady();

  const rules = ref(
    (options.initialRules ?? ["workspaces/ws/qualityRules/r1"]).map(rule)
  );
  const loading = ref(options.initialLoading ?? false);
  const canCreate = computed(() => options.canCreate ?? true);

  const Harness = defineComponent({
    setup() {
      return {
        dialogs: useTargetWorkspaceDialogs({ rules, loading, canCreate }),
      };
    },
    template: "<div />",
  });
  const wrapper = mount(Harness, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return {
    router,
    rules,
    loading,
    wrapper,
    dialogs: (
      wrapper.vm as unknown as {
        dialogs: ReturnType<typeof useTargetWorkspaceDialogs>;
      }
    ).dialogs,
  };
}

beforeEach(() => {
  notifyInfo.mockClear();
});

describe("useTargetWorkspaceDialogs", () => {
  it("opens a blank create from the header", async () => {
    const { dialogs } = await mountDialogs();

    dialogs.openComposer();

    expect(dialogs.composerOpen.value).toBe(true);
    expect(dialogs.editingRule.value).toBeUndefined();
    expect(dialogs.composerSuggestion.value).toBeUndefined();
  });

  it("keeps the draft when opened from an AI suggestion", async () => {
    const { dialogs } = await mountDialogs();
    const draft = create(QualitySuggestionSchema, {
      name: "suggestions/s1",
    });

    dialogs.openComposer(draft);

    expect(dialogs.composerOpen.value).toBe(true);
    expect(dialogs.composerSuggestion.value?.name).toBe(draft.name);
  });

  it("fixes the composer to the rule an edit names", async () => {
    const { dialogs } = await mountDialogs();
    const target = rule("workspaces/ws/qualityRules/r2");

    dialogs.openEditor(target);

    expect(dialogs.composerOpen.value).toBe(true);
    expect(dialogs.editingRule.value?.name).toBe(target.name);
    expect(dialogs.composerSuggestion.value).toBeUndefined();
  });

  it("seeds a section's scope and column", async () => {
    const { dialogs } = await mountDialogs();

    dialogs.openAddCheck({ scope: "column", column: "email" });

    expect(dialogs.composerOpen.value).toBe(true);
    expect(dialogs.composerScope.value).toBe("column");
    expect(dialogs.composerInitialColumn.value).toBe("email");
  });

  it("opens the check sheet and drops its query key when closed", async () => {
    const { dialogs, router } = await mountDialogs({
      check: "workspaces/ws/qualityRules/r1",
    });
    await nextTick();

    expect(dialogs.selectedCheck.value).toBe("workspaces/ws/qualityRules/r1");
    dialogs.closeCheck();
    await flushPromises();

    expect(dialogs.selectedCheck.value).toBe("");
    expect(router.currentRoute.value.query.check).toBeUndefined();
  });

  it("opens a check named in the URL once the rules arrive", async () => {
    const { dialogs, rules, loading } = await mountDialogs(
      { check: "workspaces/ws/qualityRules/r1" },
      { initialRules: [], initialLoading: true }
    );
    expect(dialogs.selectedCheck.value).toBe("");

    rules.value = [rule("workspaces/ws/qualityRules/r1")];
    loading.value = false;
    await nextTick();
    await flushPromises();

    expect(dialogs.selectedCheck.value).toBe("workspaces/ws/qualityRules/r1");
  });

  it("drops a stale check name without an error", async () => {
    const { dialogs, router } = await mountDialogs({
      check: "workspaces/ws/qualityRules/missing",
    });
    await flushPromises();

    expect(dialogs.selectedCheck.value).toBe("");
    expect(router.currentRoute.value.query.check).toBeUndefined();
  });

  it("opens the composer for a start-monitoring link and consumes the flag", async () => {
    const { dialogs, router } = await mountDialogs({ add: "1" });
    await flushPromises();

    expect(dialogs.composerOpen.value).toBe(true);
    expect(router.currentRoute.value.query.add).toBeUndefined();
  });

  it("explains why a start-monitoring link cannot create", async () => {
    const { dialogs, router } = await mountDialogs(
      { add: "1" },
      { canCreate: false }
    );
    await flushPromises();

    expect(dialogs.composerOpen.value).toBe(false);
    expect(router.currentRoute.value.query.add).toBeUndefined();
    expect(notifyInfo).toHaveBeenCalledOnce();
  });

  it("closes the sheet when the drawer reports it closed", async () => {
    const { dialogs } = await mountDialogs();
    dialogs.openCheck(rule("workspaces/ws/qualityRules/r1"));

    dialogs.onCheckOpenChange(true);
    expect(dialogs.selectedCheck.value).toBe("workspaces/ws/qualityRules/r1");

    dialogs.onCheckOpenChange(false);
    expect(dialogs.selectedCheck.value).toBe("");
  });

  it("does not replace the URL when closing an untouched sheet", async () => {
    const { dialogs, router } = await mountDialogs();
    const replace = vi.spyOn(router, "replace");

    dialogs.closeCheck();
    await flushPromises();

    expect(replace).not.toHaveBeenCalled();
  });

  it("keeps the check path when add=1 names a check too", async () => {
    const { dialogs, router } = await mountDialogs({
      add: "1",
      check: "workspaces/ws/qualityRules/r1",
    });
    await flushPromises();

    // The deep link's own path wins: no blank composer over the named check.
    expect(dialogs.composerOpen.value).toBe(false);
    expect(dialogs.selectedCheck.value).toBe("workspaces/ws/qualityRules/r1");
    expect(router.currentRoute.value.query.add).toBeUndefined();
  });

  it("ignores an edit link for a check this target does not have", async () => {
    const { dialogs, router } = await mountDialogs({
      check: "workspaces/ws/qualityRules/missing",
      edit: "1",
    });
    await flushPromises();

    expect(dialogs.composerOpen.value).toBe(false);
    expect(dialogs.editingRule.value).toBeUndefined();
    expect(router.currentRoute.value.query.edit).toBeUndefined();
  });

  it("turns the legacy edit link into an editor and closes the sheet", async () => {
    const { dialogs, router } = await mountDialogs({
      check: "workspaces/ws/qualityRules/r1",
      edit: "1",
    });
    await flushPromises();

    expect(dialogs.composerOpen.value).toBe(true);
    expect(dialogs.editingRule.value?.name).toBe(
      "workspaces/ws/qualityRules/r1"
    );
    expect(dialogs.selectedCheck.value).toBe("");
    expect(router.currentRoute.value.query.edit).toBeUndefined();
    expect(router.currentRoute.value.query.check).toBeUndefined();
  });
});
