import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  type QualityRuleResult,
  QualityRuleResultSchema,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityStatusKey } from "@/utils/dataQuality";
import { formatDateTime } from "@/utils/datetime";
import QualityResultStrip from "./QualityResultStrip.vue";

const BASE_TIME = new Date("2026-10-07T09:30:00Z");
const LOCALE = "en-US";

/** A result whose finish time is `minute` minutes after the base instant. */
function result(name: string, status: QualityStatus, minute: number) {
  return create(QualityRuleResultSchema, {
    name: `workspaces/ws/qualityRuleResults/${name}`,
    status,
    finishTime: timestampFromDate(
      new Date(BASE_TIME.getTime() + minute * 60_000)
    ),
  });
}

/** The square's accessible name, from the same helpers the component uses. */
function squareLabel(item: QualityRuleResult): string {
  return `${i18n.global.t(qualityStatusKey(item.status))} · ${formatDateTime(item.finishTime, LOCALE)}`;
}

function mountStrip(props: {
  results: QualityRuleResult[];
  label?: string;
  limit?: number;
  selectedName?: string;
}) {
  return mount(QualityResultStrip, {
    props: { label: "Recent executions", ...props },
    global: { plugins: [i18n] },
  });
}

function buttonLabels(wrapper: ReturnType<typeof mountStrip>): string[] {
  return wrapper
    .findAll("button")
    .map((button) => button.attributes("aria-label") ?? "");
}

describe("QualityResultStrip", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.global.locale.value = LOCALE;
  });

  it("renders newest-first results oldest to newest", () => {
    const newest = result("r-newest", QualityStatus.FAIL, 2);
    const middle = result("r-middle", QualityStatus.WARN, 1);
    const oldest = result("r-oldest", QualityStatus.PASS, 0);

    // The API returns the newest result first; the strip must show it last.
    const wrapper = mountStrip({ results: [newest, middle, oldest] });

    expect(buttonLabels(wrapper)).toEqual([
      squareLabel(oldest),
      squareLabel(middle),
      squareLabel(newest),
    ]);
    expect(wrapper.find('[role="list"]').attributes("aria-label")).toBe(
      "Recent executions"
    );
  });

  it("keeps only the newest results when limit truncates", () => {
    const fixtures = [0, 1, 2, 3, 4].map((minute) =>
      result(`r-${minute}`, QualityStatus.PASS, minute)
    );
    const wrapper = mountStrip({ results: fixtures, limit: 3 });

    // The oldest two results are dropped, then the window is reversed.
    expect(buttonLabels(wrapper)).toEqual([
      squareLabel(fixtures[2]),
      squareLabel(fixtures[1]),
      squareLabel(fixtures[0]),
    ]);
  });

  it("shows at most 20 results by default", () => {
    const fixtures = Array.from({ length: 25 }, (_, minute) =>
      result(`r-${minute}`, QualityStatus.PASS, minute)
    );
    const wrapper = mountStrip({ results: fixtures });

    const labels = buttonLabels(wrapper);
    expect(labels).toHaveLength(20);
    expect(labels[0]).toBe(squareLabel(fixtures[19]));
    expect(labels[19]).toBe(squareLabel(fixtures[0]));
  });

  it("emits the clicked result", async () => {
    const newest = result("r-newest", QualityStatus.FAIL, 2);
    const oldest = result("r-oldest", QualityStatus.PASS, 0);
    const wrapper = mountStrip({ results: [newest, oldest] });

    // The first square is the oldest result, because the strip reads left to right.
    await wrapper.findAll("button")[0].trigger("click");

    expect(wrapper.emitted("select")).toHaveLength(1);
    expect(wrapper.emitted("select")?.[0]?.[0]).toEqual(oldest);
  });

  it("names each square with its status and absolute finish time", () => {
    const newest = result("r-newest", QualityStatus.ERROR, 2);
    const wrapper = mountStrip({ results: [newest] });

    const button = wrapper.find("button");
    const finishTime = formatDateTime(newest.finishTime, LOCALE);
    expect(button.attributes("title")).toBe(
      `${i18n.global.t("dataQuality.status.error")} · ${finishTime}`
    );
    expect(button.attributes("aria-label")).toBe(
      `${i18n.global.t("dataQuality.status.error")} · ${finishTime}`
    );
  });

  it("emphasizes the newest square unless a selected one overrides it", () => {
    const newest = result("r-newest", QualityStatus.FAIL, 2);
    const middle = result("r-middle", QualityStatus.WARN, 1);
    const oldest = result("r-oldest", QualityStatus.PASS, 0);

    const wrapper = mountStrip({
      results: [newest, middle, oldest],
      selectedName: middle.name,
    });
    const [oldestButton, middleButton, newestButton] =
      wrapper.findAll("button");
    expect(oldestButton.classes()).not.toContain("ring-primary");
    expect(oldestButton.classes()).not.toContain("ring-foreground/20");
    expect(middleButton.classes()).toContain("ring-primary");
    expect(middleButton.classes()).not.toContain("ring-foreground/20");
    expect(newestButton.classes()).toContain("ring-foreground/20");
    expect(newestButton.classes()).not.toContain("ring-primary");

    // A selected newest square keeps the primary ring, not both rings.
    const selectedNewest = mountStrip({
      results: [newest, middle, oldest],
      selectedName: newest.name,
    });
    const newestSelectedButton = selectedNewest.findAll("button")[2];
    expect(newestSelectedButton.classes()).toContain("ring-primary");
    expect(
      selectedNewest
        .findAll("button")
        .some((button) => button.classes().includes("ring-foreground/20"))
    ).toBe(false);
  });

  it("renders nothing when there are no results", () => {
    const wrapper = mountStrip({ results: [] });

    expect(wrapper.findAll("button")).toHaveLength(0);
    expect(wrapper.find('[role="list"]').exists()).toBe(false);
  });

  it("colors each status with its theme token", () => {
    const cases: Array<[QualityStatus, string]> = [
      [QualityStatus.PASS, "hsl(var(--success))"],
      [QualityStatus.WARN, "hsl(var(--warning))"],
      [QualityStatus.FAIL, "hsl(var(--destructive))"],
      [QualityStatus.ERROR, "hsl(var(--danger))"],
      [QualityStatus.SKIPPED, "hsl(var(--muted-foreground) / 0.35)"],
      [QualityStatus.NOT_EVALUATED, "hsl(var(--border))"],
      [QualityStatus.UNSPECIFIED, "hsl(var(--muted))"],
    ];
    for (const [status, color] of cases) {
      const wrapper = mountStrip({
        results: [result(`r-${status}`, status, 0)],
      });
      expect(wrapper.find("button").attributes("style")).toContain(
        `background-color: ${color}`
      );
      wrapper.unmount();
    }
  });
});
