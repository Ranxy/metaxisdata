import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import LineageNodeDetailPanel, {
  type NodeDetailSummary,
} from "./LineageNodeDetailPanel.vue";

function summary(
  overrides: Partial<NodeDetailSummary> = {}
): NodeDetailSummary {
  return {
    guid: "instances/i/databases/d/schemas/s/tables/t",
    label: "orders",
    fullLabel: "prod > shop > orders",
    scopeLabel: "prod / shop",
    scopeColor: "#64748b",
    isExternal: false,
    metaTypeLabel: "Table",
    upstreamCount: 3,
    downstreamCount: 2,
    columns: ["id", "total"],
    externalNamespace: "",
    externalDatasetType: "",
    ...overrides,
  };
}

function run(guid = "runs/r1") {
  return { guid, label: `Run ${guid}`, updatedAtLabel: "2 hours ago" };
}

function mountPanel(props: Record<string, unknown>) {
  return mount(LineageNodeDetailPanel, {
    props,
    global: { plugins: [i18n] },
  });
}

describe("LineageNodeDetailPanel", () => {
  it("shows the selection hint while nothing is selected", () => {
    const wrapper = mountPanel({ summary: null });

    expect(wrapper.text()).toContain(
      i18n.global.t("lineageGraph.selectNodeHint")
    );
  });

  it("prints the object's identity and relation counts", () => {
    const wrapper = mountPanel({ summary: summary() });

    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("prod > shop > orders");
    expect(wrapper.text()).toContain("prod / shop");
    expect(wrapper.text()).toContain("3");
    expect(wrapper.text()).toContain("2");
  });

  it("shows external dataset facts only for an external object", () => {
    const internal = mountPanel({ summary: summary() });
    expect(internal.text()).not.toContain("openlineage.datasetType");

    const external = mountPanel({
      summary: summary({
        isExternal: true,
        externalNamespace: "snowflake://acct",
        externalDatasetType: "TABLE",
      }),
    });
    expect(external.text()).toContain("snowflake://acct");
    expect(external.text()).toContain("TABLE");
    expect(
      external
        .findAll("button")
        .some((button) => button.attributes("disabled") !== undefined)
    ).toBe(true);
  });

  it("reports the selected field and the trail's reach", () => {
    const wrapper = mountPanel({
      summary: summary(),
      columnContext: "total",
      fieldTrail: {
        upstream: { nodeIds: new Set(["a", "b"]), relationCount: 2 },
        downstream: { nodeIds: new Set(["c"]), relationCount: 1 },
        truncated: true,
      },
      dimsOffTrail: true,
    });

    expect(wrapper.text()).toContain("total");
    expect(wrapper.text()).toContain(
      i18n.global.t("lineageGraph.fieldTrailTruncated")
    );
    const fit = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("lineageGraph.fieldTrailFit"))
      );
    expect(fit?.attributes("disabled")).toBeUndefined();
  });

  it("lists the related runs and opens one", async () => {
    const wrapper = mountPanel({
      summary: summary(),
      relatedRuns: [run()],
    });

    const button = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Run runs/r1"));
    await button?.trigger("click");

    expect(wrapper.emitted("open-run")?.[0]).toEqual(["runs/r1"]);
  });

  it("shows the empty related-runs sentence instead of an empty list", () => {
    const wrapper = mountPanel({ summary: summary(), relatedRuns: [] });

    expect(wrapper.text()).toContain(
      i18n.global.t("lineageGraph.noRelatedRuns")
    );
  });

  it("emits the detail actions", async () => {
    const wrapper = mountPanel({
      summary: summary(),
      canOpenColumnLineage: true,
    });
    const button = (labelKey: string) =>
      wrapper
        .findAll("button")
        .find((candidate) =>
          candidate.text().includes(i18n.global.t(labelKey))
        );

    await button("lineageGraph.closeDetail")?.trigger("click");
    await button("lineageGraph.refocusGraph")?.trigger("click");
    await button("metadataBrowser.viewSchema")?.trigger("click");
    await button("lineageGraph.openColumnLineage")?.trigger("click");
    await button("openlineage.openMetadata")?.trigger("click");

    expect(wrapper.emitted("close")).toHaveLength(1);
    expect(wrapper.emitted("refocus")).toHaveLength(1);
    expect(wrapper.emitted("open-schema")?.[0]).toEqual([summary().guid]);
    expect(wrapper.emitted("open-column-lineage")).toHaveLength(1);
    expect(wrapper.emitted("open-metadata")).toHaveLength(1);
  });

  it("disables column lineage when the node has no fields", () => {
    const wrapper = mountPanel({
      summary: summary(),
      canOpenColumnLineage: false,
    });
    const button = wrapper
      .findAll("button")
      .find((candidate) =>
        candidate
          .text()
          .includes(i18n.global.t("lineageGraph.openColumnLineage"))
      );

    expect(button?.attributes("disabled")).toBeDefined();
  });

  it("hides the trail block when no field is selected", () => {
    const wrapper = mountPanel({ summary: summary(), fieldTrail: null });

    expect(wrapper.text()).not.toContain(
      i18n.global.t("lineageGraph.fieldTrailHint")
    );
  });
});
