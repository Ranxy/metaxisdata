import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { computed, defineComponent } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { useLineageSelection } from "./useLineageSelection";

async function mountSelection(
  initialQuery: Record<string, string> = {},
  currentGuid = "root"
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/", component: { template: "<div />" } }],
  });
  await router.push({ path: "/", query: initialQuery });
  await router.isReady();
  const fetchLineage = vi.fn(async () => {});
  const onChanged = vi.fn();
  const Harness = defineComponent({
    setup() {
      return {
        selection: useLineageSelection({
          currentGuid: computed(() => currentGuid),
          fetchLineage,
          onChanged,
        }),
      };
    },
    template: "<div />",
  });
  const wrapper = mount(Harness, { global: { plugins: [router] } });
  return {
    router,
    fetchLineage,
    onChanged,
    selection: (
      wrapper.vm as unknown as {
        selection: ReturnType<typeof useLineageSelection>;
      }
    ).selection,
  };
}

describe("useLineageSelection", () => {
  it("reads the selected node from the URL", async () => {
    const { selection } = await mountSelection({ node: "instances/i" });

    expect(selection.selectedNodeGuid.value).toBe("instances/i");
  });

  it("writes the selected node into the URL, keeping the rest", async () => {
    const { router, selection } = await mountSelection({ metaType: "18" });

    selection.setSelectedNode("instances/i");
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({
      metaType: "18",
      node: "instances/i",
    });

    selection.setSelectedNode(null);
    await flushPromises();
    expect(router.currentRoute.value.query).toEqual({ metaType: "18" });
  });

  it("prefetches a non-root node's relations and asks for a redraw", async () => {
    const { fetchLineage, onChanged, selection } = await mountSelection();

    await selection.handleSelectNode("instances/i");
    await flushPromises();

    expect(fetchLineage).toHaveBeenCalledWith("instances/i");
    expect(onChanged).toHaveBeenCalledOnce();
  });

  it("does not prefetch the root and closes on a second click", async () => {
    const { router, fetchLineage, selection } = await mountSelection();

    await selection.handleSelectNode("root");
    await flushPromises();
    expect(fetchLineage).not.toHaveBeenCalled();

    await selection.handleSelectNode("root");
    await flushPromises();
    expect(router.currentRoute.value.query.node).toBeUndefined();
  });

  it("selects a field together with its node", async () => {
    const { router, fetchLineage, onChanged, selection } =
      await mountSelection();

    await selection.handleSelectColumn("instances/i", "orders.id");
    await flushPromises();

    expect(selection.selectedColumnGuid.value).toBe("instances/i");
    expect(selection.selectedColumnName.value).toBe("orders.id");
    expect(selection.selectedColumnContext.value).toBe("orders.id");
    expect(router.currentRoute.value.query.node).toBe("instances/i");
    expect(fetchLineage).toHaveBeenCalledOnce();
    expect(onChanged).toHaveBeenCalledOnce();
  });

  it("toggles the same field off", async () => {
    const { onChanged, selection } = await mountSelection();

    await selection.handleSelectColumn("instances/i", "orders.id");
    await selection.handleSelectColumn("instances/i", "orders.id");

    expect(selection.selectedColumnGuid.value).toBeNull();
    expect(selection.selectedColumnName.value).toBeNull();
    expect(onChanged).toHaveBeenCalledTimes(2);
  });

  it("keeps the field context only while it belongs to the selected node", async () => {
    const { selection } = await mountSelection({ node: "other" });

    await selection.markSelectedColumn("instances/i", "orders.id");
    await flushPromises();

    // markSelectedColumn selects its node, so the context belongs to it.
    expect(selection.selectedColumnContext.value).toBe("orders.id");

    selection.clearColumnSelection();
    expect(selection.selectedColumnContext.value).toBeNull();
  });

  it("does not prefetch the root's relations for a field", async () => {
    const { fetchLineage, selection } = await mountSelection({}, "root");

    await selection.markSelectedColumn("root", "orders.id");
    await flushPromises();

    expect(fetchLineage).not.toHaveBeenCalled();
    expect(selection.selectedNodeGuid.value).toBe("root");
  });
});
