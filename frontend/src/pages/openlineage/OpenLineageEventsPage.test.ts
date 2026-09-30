import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import OpenLineageEventsPage from "./OpenLineageEventsPage.vue";

const mocks = vi.hoisted(() => ({
  listOpenLineageRuns: vi.fn(),
  listOpenLineageFilterOptions: vi.fn(),
}));

vi.mock("@/api/openlineage", () => ({
  listOpenLineageRuns: mocks.listOpenLineageRuns,
  listOpenLineageFilterOptions: mocks.listOpenLineageFilterOptions,
}));

// The search bar loads the workspace environments on mount.
vi.mock("@/api/environment", () => ({
  listAllEnvironments: vi.fn().mockResolvedValue([]),
  environmentId: (name: string) => name,
}));

const Dummy = { template: "<div />" };

async function mountEvents(query: Record<string, string>) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/openlineage/events",
        name: "OpenLineageEvents",
        component: Dummy,
      },
      {
        path: "/openlineage/events/:guid",
        name: "OpenLineageRunDetail",
        component: Dummy,
      },
      { path: "/openlineage/jobs", name: "OpenLineageRuns", component: Dummy },
    ],
  });
  await router.push({ path: "/openlineage/events", query });
  await router.isReady();

  const wrapper = mount(OpenLineageEventsPage, {
    global: { plugins: [router, i18n, createPinia()] },
  });
  await flushPromises();
  return wrapper;
}

function searchBarValue(wrapper: Awaited<ReturnType<typeof mountEvents>>) {
  const input = wrapper.find('input[type="text"]').element as HTMLInputElement;
  return input.value;
}

function clearButton(wrapper: Awaited<ReturnType<typeof mountEvents>>) {
  return wrapper.findAll("button").find((b) => b.text() === "Clear filters");
}

describe("OpenLineageEventsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listOpenLineageRuns.mockResolvedValue({
      runs: [],
      nextPageToken: "",
    });
    mocks.listOpenLineageFilterOptions.mockResolvedValue({});
  });

  // The job list's "open events" action links here with search and namespace.
  it("filters by the search and namespace the URL carries", async () => {
    await mountEvents({ search: "dag.extract", namespace: "airflow" });

    expect(mocks.listOpenLineageRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        search: "dag.extract",
        jobNamespace: "airflow",
      })
    );
  });

  it("shows the URL filters as a pill and in the search box", async () => {
    const wrapper = await mountEvents({
      search: "dag.extract",
      namespace: "airflow",
    });

    expect(wrapper.text()).toContain("airflow");
    expect(searchBarValue(wrapper)).toBe("dag.extract");
  });

  it("asks for no filter when the URL carries none", async () => {
    await mountEvents({});

    expect(mocks.listOpenLineageRuns).toHaveBeenCalledWith(
      expect.objectContaining({ search: "", jobNamespace: "" })
    );
  });

  it("clears the bar with the page's clear button", async () => {
    const wrapper = await mountEvents({
      search: "dag.extract",
      namespace: "airflow",
    });
    mocks.listOpenLineageRuns.mockClear();

    await clearButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).not.toContain("airflow");
    expect(searchBarValue(wrapper)).toBe("");
    expect(mocks.listOpenLineageRuns).toHaveBeenCalledWith(
      expect.objectContaining({ search: "", jobNamespace: "" })
    );
  });
});
