import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  NullPercentConfigSchema,
  type QualityRule,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleType,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetCoverageCard from "./TargetCoverageCard.vue";

const mocks = vi.hoisted(() => ({
  listAllMetadata: vi.fn(),
}));

vi.mock("@/api/database", () => ({
  listAllMetadata: mocks.listAllMetadata,
}));

const TABLE = "inst1;db1;public;orders";
const LOCALE = "en-US";

/** A column child of the target, shaped as `listAllMetadata` returns it. */
function column(name: string) {
  return {
    guid: `${TABLE};${name}`,
    type: { case: "columnMetadata", value: { name } },
  };
}

function columnRule(columnName: string): QualityRule {
  return create(QualityRuleSchema, {
    ruleType: QualityRuleType.NULL_PERCENT,
    target: create(QualityTargetSchema, { columns: [columnName] }),
    config: create(QualityRuleConfigSchema, {
      config: {
        case: "nullPercent",
        value: create(NullPercentConfigSchema, { column: columnName }),
      },
    }),
  });
}

/** A rule that names no column: it does not cover any single one. */
function datasetRule(): QualityRule {
  return create(QualityRuleSchema, {
    target: create(QualityTargetSchema, {}),
  });
}

function mountCard(
  props: {
    targetGuid?: string;
    rules?: QualityRule[];
    canCreate?: boolean;
  } = {}
) {
  return mount(TargetCoverageCard, {
    props: {
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      rules: [],
      canCreate: false,
      ...props,
    },
    global: { plugins: [i18n] },
  });
}

function t(key: string, params: Record<string, unknown> = {}) {
  return i18n.global.t(key, params);
}

describe("TargetCoverageCard", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.global.locale.value = LOCALE;
    mocks.listAllMetadata.mockReset();
  });

  it("summarizes coverage and counts the checks behind each covered column", async () => {
    mocks.listAllMetadata.mockResolvedValue([
      column("email"),
      column("id"),
      column("name"),
    ]);
    const wrapper = mountCard({
      rules: [
        columnRule("email"),
        columnRule("email"),
        columnRule("id"),
        datasetRule(),
      ],
    });
    await flushPromises();

    expect(mocks.listAllMetadata).toHaveBeenCalledWith({
      parentGuid: TABLE,
      metaType: MetaType.COLUMN,
    });
    expect(wrapper.text()).toContain(
      t("dataQuality.coverage.coveredSummary", { covered: 2, total: 3 })
    );

    const progress = wrapper.find('[role="progressbar"]');
    expect(progress.attributes("aria-valuenow")).toBe("2");
    expect(progress.attributes("aria-valuemax")).toBe("3");
    const bar = progress.find("div");
    expect(bar.attributes("style")).toContain("width: 67%");
    expect(bar.attributes("style")).toContain("hsl(var(--success))");

    const lists = wrapper.findAll("ul");
    const covered = lists[0].findAll("li");
    expect(covered).toHaveLength(2);
    expect(covered[0].text()).toContain("email");
    expect(covered[0].text()).toContain("2");
    expect(covered[1].text()).toContain("id");
    expect(covered[1].text()).toContain("1");

    const uncovered = lists[1].findAll("li");
    expect(uncovered).toHaveLength(1);
    expect(uncovered[0].text()).toBe("name");
    expect(uncovered[0].find("div").attributes("title")).toBe(
      t("dataQuality.coverage.uncovered")
    );
  });

  it("says so when every column has a check", async () => {
    mocks.listAllMetadata.mockResolvedValue([column("email"), column("id")]);
    const wrapper = mountCard({
      rules: [columnRule("email"), columnRule("id")],
    });
    await flushPromises();

    expect(wrapper.text()).toContain(
      t("dataQuality.coverage.coveredSummary", { covered: 2, total: 2 })
    );
    expect(wrapper.text()).toContain(t("dataQuality.coverage.uncoveredEmpty"));
    expect(wrapper.findAll('[title="No checks"]')).toHaveLength(0);
  });

  it("caps the uncovered chips at twelve and counts the rest", async () => {
    mocks.listAllMetadata.mockResolvedValue(
      Array.from({ length: 15 }, (_, index) => column(`column_${index}`))
    );
    const wrapper = mountCard();
    await flushPromises();

    const uncoveredList = wrapper.findAll("ul")[0];
    const items = uncoveredList.findAll("li");
    expect(items).toHaveLength(13);
    expect(uncoveredList.findAll("div")).toHaveLength(12);
    expect(items[12].text()).toBe("+3");
  });

  it("degrades to the failure message and re-reads on retry", async () => {
    mocks.listAllMetadata.mockRejectedValueOnce(new Error("boom"));
    const wrapper = mountCard();
    await flushPromises();

    expect(wrapper.text()).toContain(t("dataQuality.coverage.loadFailed"));
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(false);

    mocks.listAllMetadata.mockResolvedValue([column("email"), column("id")]);
    const retry = wrapper
      .findAll("button")
      .find((button) => button.text() === t("common.retry"));
    expect(retry).toBeDefined();
    await retry!.trigger("click");
    await flushPromises();

    expect(wrapper.text()).not.toContain(t("dataQuality.coverage.loadFailed"));
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(true);
    expect(wrapper.text()).toContain("email");
    expect(mocks.listAllMetadata).toHaveBeenCalledTimes(2);
  });

  it("shows a light loading state while the columns load", async () => {
    mocks.listAllMetadata.mockReturnValue(new Promise(() => {}));
    const wrapper = mountCard();

    expect(wrapper.text()).toContain(t("common.loading"));
    expect(wrapper.find('[role="progressbar"]').exists()).toBe(false);
  });

  it("renders no coverage body when the target has no columns", async () => {
    mocks.listAllMetadata.mockResolvedValue([]);
    const wrapper = mountCard();
    await flushPromises();

    expect(wrapper.find('[role="progressbar"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain(t("dataQuality.coverage.loadFailed"));
    // The description stays; there is no second empty message.
    expect(wrapper.text()).toContain(t("dataQuality.coverage.description"));
  });

  it("emits create from the add-rule button only when allowed", async () => {
    mocks.listAllMetadata.mockResolvedValue([]);
    const wrapper = mountCard({ canCreate: true });
    await flushPromises();

    const addRule = wrapper
      .findAll("button")
      .find((button) => button.text() === t("dataQuality.coverage.addRule"));
    expect(addRule).toBeDefined();
    await addRule!.trigger("click");
    expect(wrapper.emitted("create")).toHaveLength(1);

    const readOnly = mountCard({ canCreate: false });
    await flushPromises();
    expect(
      readOnly
        .findAll("button")
        .some((button) => button.text() === t("dataQuality.coverage.addRule"))
    ).toBe(false);
  });

  it("re-reads the columns when the target changes", async () => {
    mocks.listAllMetadata.mockResolvedValue([column("email")]);
    const wrapper = mountCard();
    await flushPromises();

    mocks.listAllMetadata.mockResolvedValue([column("id"), column("name")]);
    await wrapper.setProps({ targetGuid: "inst1;db1;public;customers" });
    await flushPromises();

    expect(mocks.listAllMetadata).toHaveBeenCalledTimes(2);
    expect(mocks.listAllMetadata).toHaveBeenLastCalledWith({
      parentGuid: "inst1;db1;public;customers",
      metaType: MetaType.COLUMN,
    });
    expect(wrapper.text()).toContain(
      t("dataQuality.coverage.coveredSummary", { covered: 0, total: 2 })
    );
  });
});
