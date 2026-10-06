import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { OpenLineageRunSchema } from "@/types/proto-es/v1/openlineage_service_pb";
import OpenLineageRunDetailPage from "./OpenLineageRunDetailPage.vue";

const mocks = vi.hoisted(() => ({
  getOpenLineageRun: vi.fn(),
}));

vi.mock("@/api/openlineage", () => ({
  getOpenLineageRun: mocks.getOpenLineageRun,
}));

const Dummy = { template: "<div />" };

async function mountRun(rawPayload: string, airflowRunLogUrl = "") {
  mocks.getOpenLineageRun.mockResolvedValue(
    create(OpenLineageRunSchema, {
      guid: "run-1",
      jobName: "e2e_02_pg_transform.dwd_ddl",
      eventType: "COMPLETE",
      rawPayload,
      airflowRunLogUrl,
    })
  );

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/openlineage/events/:guid",
        name: "OpenLineageRunDetail",
        component: Dummy,
      },
      {
        path: "/openlineage/events",
        name: "OpenLineageEvents",
        component: Dummy,
      },
      {
        path: "/openlineage/datasets",
        name: "OpenLineageDatasets",
        component: Dummy,
      },
      {
        path: "/openlineage/jobs/:guid",
        name: "OpenLineageTaskDetail",
        component: Dummy,
      },
      { path: "/lineage", name: "LineageGraph", component: Dummy },
    ],
  });
  await router.push("/openlineage/events/run-1");
  await router.isReady();

  const wrapper = mount(OpenLineageRunDetailPage, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return wrapper;
}

// The Airflow extractor records a statement it could not parse as a run facet
// and drops it from the run's datasets, so the event carries SQL and no dataset.
function payloadWithExtractionError(): string {
  return JSON.stringify({
    run: {
      runId: "0199a1f5-d2f6-7e58-9f19-3e02bff4e6be",
      facets: {
        extractionError: {
          totalTasks: 1,
          failedTasks: 1,
          errors: [
            {
              task: "REFRESH MATERIALIZED VIEW e2e_dwd.mv_daily_sales;",
              taskNumber: 6,
              errorMessage:
                "Expected: an SQL statement, found: REFRESH at Line: 1, Column: 1",
            },
          ],
        },
      },
    },
    job: {
      namespace: "default",
      name: "e2e_02_pg_transform.dwd_load",
      facets: {
        sql: { query: "REFRESH MATERIALIZED VIEW e2e_dwd.mv_daily_sales;" },
      },
    },
    inputs: [],
    outputs: [],
  });
}

function payloadWithUnparsedSQLOnly(): string {
  return JSON.stringify({
    job: {
      namespace: "default",
      name: "dag.task",
      facets: { sql: { query: "select 1" } },
    },
    inputs: [],
    outputs: [],
  });
}

describe("OpenLineageRunDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("names each statement the extractor could not parse, beside its reason", async () => {
    const wrapper = await mountRun(payloadWithExtractionError());

    expect(wrapper.text()).toContain("Some SQL statements could not be parsed");
    expect(wrapper.text()).toContain(
      "Expected: an SQL statement, found: REFRESH at Line: 1, Column: 1"
    );
    expect(wrapper.text()).toContain(
      "REFRESH MATERIALIZED VIEW e2e_dwd.mv_daily_sales;"
    );
  });

  it("prefers the specific alert over the generic one", async () => {
    const wrapper = await mountRun(payloadWithExtractionError());

    expect(wrapper.text()).toContain("Some SQL statements could not be parsed");
    expect(wrapper.text()).not.toContain("No lineage could be extracted");
  });

  it("keeps the generic alert for events with no extractionError facet", async () => {
    const wrapper = await mountRun(payloadWithUnparsedSQLOnly());

    expect(wrapper.text()).toContain("No lineage could be extracted");
    expect(wrapper.text()).not.toContain(
      "Some SQL statements could not be parsed"
    );
  });

  it("shows no alert for an event that extracted its lineage", async () => {
    const wrapper = await mountRun(
      JSON.stringify({
        job: {
          namespace: "default",
          name: "dag.task",
          facets: { sql: { query: "select 1" } },
        },
        inputs: [],
        outputs: [
          { namespace: "postgres://localhost:5432", name: "e2e.e2e_dwd.fact" },
        ],
      })
    );

    expect(wrapper.text()).not.toContain("No lineage could be extracted");
    expect(wrapper.text()).not.toContain(
      "Some SQL statements could not be parsed"
    );
  });

  // The server derives this URL from a facet anyone with an ingestion key can
  // write, so the page must not bind it as it arrives: a `javascript:` value
  // would run in this origin, where every ConnectRPC call passes the CSRF check.
  it("renders no link for a URL that is not a web address", async () => {
    const wrapper = await mountRun("", "javascript:alert(document.cookie)");

    expect(wrapper.find("a[target='_blank']").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Open Run Log in Airflow");
  });

  it("renders the derived link when it is a web address", async () => {
    const wrapper = await mountRun(
      "",
      "http://airflow.example.com:8080/dags/x/runs/1"
    );

    const link = wrapper.find("a[target='_blank']");
    expect(link.exists()).toBe(true);
    expect(link.attributes("href")).toBe(
      "http://airflow.example.com:8080/dags/x/runs/1"
    );
    expect(wrapper.text()).toContain("Open Run Log in Airflow");
  });
});
