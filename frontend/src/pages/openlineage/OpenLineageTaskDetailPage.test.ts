import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { OpenLineageTaskSchema } from "@/types/proto-es/v1/openlineage_service_pb";
import OpenLineageTaskDetailPage from "./OpenLineageTaskDetailPage.vue";

const mocks = vi.hoisted(() => ({
  getOpenLineageTask: vi.fn(),
  listOpenLineageRuns: vi.fn(),
}));

vi.mock("@/api/openlineage", () => ({
  getOpenLineageTask: mocks.getOpenLineageTask,
  listOpenLineageRuns: mocks.listOpenLineageRuns,
}));

const Dummy = { template: "<div />" };

async function mountTask(airflowDagUrl: string) {
  mocks.getOpenLineageTask.mockResolvedValue(
    create(OpenLineageTaskSchema, {
      guid: "task-1",
      jobName: "e2e_02_pg_transform.dwd_ddl",
      jobType: "UNSPECIFIED",
      airflowDagUrl,
    })
  );
  mocks.listOpenLineageRuns.mockResolvedValue({ runs: [] });

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/openlineage/jobs/:guid",
        name: "OpenLineageTaskDetail",
        component: Dummy,
      },
      {
        path: "/openlineage/jobs",
        name: "OpenLineageTasks",
        component: Dummy,
      },
      {
        path: "/openlineage/events/:guid",
        name: "OpenLineageRunDetail",
        component: Dummy,
      },
      { path: "/lineage", name: "LineageGraph", component: Dummy },
    ],
  });
  await router.push("/openlineage/jobs/task-1");
  await router.isReady();

  const wrapper = mount(OpenLineageTaskDetailPage, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return wrapper;
}

describe("OpenLineageTaskDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // The Dag URL is derived from the same caller-supplied facet as the run log
  // URL, so it gets the same treatment: no binding when it is not a web address.
  it("renders no link for a URL that is not a web address", async () => {
    const wrapper = await mountTask("javascript:alert(document.cookie)");

    expect(wrapper.find("a[target='_blank']").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Open in Airflow");
  });

  it("renders the derived link when it is a web address", async () => {
    const wrapper = await mountTask("http://airflow.example.com/dags/x");

    const link = wrapper.find("a[target='_blank']");
    expect(link.exists()).toBe(true);
    expect(link.attributes("href")).toBe("http://airflow.example.com/dags/x");
    expect(wrapper.text()).toContain("Open in Airflow");
  });
});
