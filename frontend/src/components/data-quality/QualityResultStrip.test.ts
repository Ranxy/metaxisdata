import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  type QualityExecution,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  qualityExecutionStateKey,
  qualityStatusKey,
} from "@/utils/dataQuality";
import { formatDateTime } from "@/utils/datetime";
import QualityResultStrip from "./QualityResultStrip.vue";

const BASE_TIME = new Date("2026-10-07T09:30:00Z");
const LOCALE = "en-US";

/** A finished execution whose finish time is `minute` minutes after the base. */
function execution(
  name: string,
  outcome: QualityStatus,
  minute: number
): QualityExecution {
  return create(QualityExecutionSchema, {
    name: `workspaces/ws/qualityExecutions/${name}`,
    state: QualityExecutionState.COMPLETED,
    outcome,
    startTime: timestampFromDate(
      new Date(BASE_TIME.getTime() + minute * 60_000)
    ),
    finishTime: timestampFromDate(
      new Date(BASE_TIME.getTime() + minute * 60_000)
    ),
  });
}

/** A still-running execution: no verdict, so the strip names its state. */
function pendingExecution(name: string): QualityExecution {
  return create(QualityExecutionSchema, {
    name: `workspaces/ws/qualityExecutions/${name}`,
    state: QualityExecutionState.RUNNING,
    outcome: QualityStatus.UNSPECIFIED,
    startTime: timestampFromDate(BASE_TIME),
  });
}

/** The square's accessible name, from the same helpers the component uses. */
function squareLabel(item: QualityExecution): string {
  return `${i18n.global.t(qualityStatusKey(item.outcome))} · ${formatDateTime(item.finishTime, LOCALE)}`;
}

function mountStrip(props: {
  executions: QualityExecution[];
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

  it("renders newest-first executions oldest to newest", () => {
    const newest = execution("r-newest", QualityStatus.FAIL, 2);
    const middle = execution("r-middle", QualityStatus.WARN, 1);
    const oldest = execution("r-oldest", QualityStatus.PASS, 0);

    // The API returns the newest execution first; the strip must show it last.
    const wrapper = mountStrip({
      executions: [newest, middle, oldest],
    });

    expect(buttonLabels(wrapper)).toEqual([
      squareLabel(oldest),
      squareLabel(middle),
      squareLabel(newest),
    ]);
    expect(wrapper.find('[role="list"]').attributes("aria-label")).toBe(
      "Recent executions"
    );
  });

  it("keeps only the newest executions when limit truncates", () => {
    const fixtures = [0, 1, 2, 3, 4].map((minute) =>
      execution(`r-${minute}`, QualityStatus.PASS, minute)
    );
    const wrapper = mountStrip({ executions: fixtures, limit: 3 });

    // The oldest two executions are dropped, then the window is reversed.
    expect(buttonLabels(wrapper)).toEqual([
      squareLabel(fixtures[2]),
      squareLabel(fixtures[1]),
      squareLabel(fixtures[0]),
    ]);
  });

  it("shows at most 20 executions by default", () => {
    const fixtures = Array.from({ length: 25 }, (_, minute) =>
      execution(`r-${minute}`, QualityStatus.PASS, minute)
    );
    const wrapper = mountStrip({ executions: fixtures });

    const labels = buttonLabels(wrapper);
    expect(labels).toHaveLength(20);
    expect(labels[0]).toBe(squareLabel(fixtures[19]));
    expect(labels[19]).toBe(squareLabel(fixtures[0]));
  });

  it("emits the clicked execution", async () => {
    const newest = execution("r-newest", QualityStatus.FAIL, 2);
    const oldest = execution("r-oldest", QualityStatus.PASS, 0);
    const wrapper = mountStrip({ executions: [newest, oldest] });

    // The first square is the oldest execution, because the strip reads left to right.
    await wrapper.findAll("button")[0].trigger("click");

    expect(wrapper.emitted("select")).toHaveLength(1);
    expect(wrapper.emitted("select")?.[0]?.[0]).toEqual(oldest);
  });

  it("names each square with its verdict and absolute finish time", () => {
    const newest = execution("r-newest", QualityStatus.ERROR, 2);
    const wrapper = mountStrip({ executions: [newest] });

    const button = wrapper.find("button");
    const finishTime = formatDateTime(newest.finishTime, LOCALE);
    expect(button.attributes("title")).toBe(
      `${i18n.global.t("dataQuality.status.error")} · ${finishTime}`
    );
    expect(button.attributes("aria-label")).toBe(
      `${i18n.global.t("dataQuality.status.error")} · ${finishTime}`
    );
  });

  it("names a pending execution by its lifecycle state, not a verdict", () => {
    const pending = pendingExecution("r-pending");
    const wrapper = mountStrip({ executions: [pending] });

    const button = wrapper.find("button");
    expect(button.attributes("aria-label")).toBe(
      `${i18n.global.t(qualityExecutionStateKey(QualityExecutionState.RUNNING))} · ${formatDateTime(pending.startTime, LOCALE)}`
    );
  });

  it("emphasizes the newest square unless a selected one overrides it", () => {
    const newest = execution("r-newest", QualityStatus.FAIL, 2);
    const middle = execution("r-middle", QualityStatus.WARN, 1);
    const oldest = execution("r-oldest", QualityStatus.PASS, 0);

    const wrapper = mountStrip({
      executions: [newest, middle, oldest],
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
      executions: [newest, middle, oldest],
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

  it("renders nothing when there are no executions", () => {
    const wrapper = mountStrip({ executions: [] });

    expect(wrapper.findAll("button")).toHaveLength(0);
    expect(wrapper.find('[role="list"]').exists()).toBe(false);
  });

  it("colors each verdict with its theme token", () => {
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
        executions: [execution(`r-${status}`, status, 0)],
      });
      expect(wrapper.find("button").attributes("style")).toContain(
        `background-color: ${color}`
      );
      wrapper.unmount();
    }
  });

  it("colors a pending execution in the info token", () => {
    const wrapper = mountStrip({ executions: [pendingExecution("r-pending")] });
    expect(wrapper.find("button").attributes("style")).toContain(
      "background-color: hsl(var(--info))"
    );
  });
});
