import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  type QualityExecutionSummary,
  QualityExecutionSummarySchema,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityStatusKey } from "@/utils/dataQuality";
import { formatDateTime } from "@/utils/datetime";
import TargetRuleHistoryStrip from "./TargetRuleHistoryStrip.vue";

const BASE_TIME = new Date("2026-10-07T09:30:00Z");
const LOCALE = "en-US";

/** A finished execution whose finish time is `minute` minutes after the base. */
function execution(
  name: string,
  status: QualityStatus,
  minute: number
): QualityExecutionSummary {
  return create(QualityExecutionSummarySchema, {
    execution: `workspaces/ws/qualityExecutions/${name}`,
    status,
    finishTime: timestampFromDate(
      new Date(BASE_TIME.getTime() + minute * 60_000)
    ),
  });
}

/** The square's accessible name, from the same helpers the component uses. */
function squareLabel(item: QualityExecutionSummary): string {
  return `${i18n.global.t(qualityStatusKey(item.status))} · ${formatDateTime(item.finishTime, LOCALE)}`;
}

async function mountStrip(props: {
  executions: QualityExecutionSummary[];
  label?: string;
  limit?: number;
}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: { template: "<div />" } },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/");
  await router.isReady();
  return mount(TargetRuleHistoryStrip, {
    props: { label: "Recent executions", ...props },
    global: { plugins: [i18n, router] },
  });
}

type Mounted = Awaited<ReturnType<typeof mountStrip>>;

function squares(wrapper: Mounted) {
  return wrapper.findAll('[role="listitem"]');
}

describe("TargetRuleHistoryStrip", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.global.locale.value = LOCALE;
  });

  it("renders newest-first executions oldest to newest", async () => {
    const newest = execution("e-newest", QualityStatus.FAIL, 2);
    const middle = execution("e-middle", QualityStatus.WARN, 1);
    const oldest = execution("e-oldest", QualityStatus.PASS, 0);

    // The API returns the newest execution first; the strip must show it last.
    const wrapper = await mountStrip({
      executions: [newest, middle, oldest],
    });

    expect(
      squares(wrapper).map((item) => item.attributes("aria-label"))
    ).toEqual([squareLabel(oldest), squareLabel(middle), squareLabel(newest)]);
    expect(wrapper.find('[role="list"]').attributes("aria-label")).toBe(
      "Recent executions"
    );
  });

  it("keeps only the newest executions when limit truncates", async () => {
    const fixtures = [0, 1, 2, 3, 4].map((minute) =>
      execution(`e-${minute}`, QualityStatus.PASS, minute)
    );
    const wrapper = await mountStrip({ executions: fixtures, limit: 3 });

    // The oldest two executions are dropped, then the window is reversed.
    expect(squares(wrapper).map((item) => item.attributes("title"))).toEqual([
      squareLabel(fixtures[2]),
      squareLabel(fixtures[1]),
      squareLabel(fixtures[0]),
    ]);
  });

  it("shows at most 10 executions by default", async () => {
    const fixtures = Array.from({ length: 12 }, (_, minute) =>
      execution(`e-${minute}`, QualityStatus.PASS, minute)
    );
    const wrapper = await mountStrip({ executions: fixtures });

    const labels = squares(wrapper).map((item) => item.attributes("title"));
    expect(labels).toHaveLength(10);
    expect(labels[0]).toBe(squareLabel(fixtures[9]));
    expect(labels[9]).toBe(squareLabel(fixtures[0]));
  });

  it("links every square to its execution detail", async () => {
    const oldest = execution("e-oldest", QualityStatus.PASS, 0);
    const newest = execution("e-newest", QualityStatus.ERROR, 1);
    const wrapper = await mountStrip({ executions: [newest, oldest] });

    // The first square is the oldest execution, because the strip reads left to right.
    expect(squares(wrapper)[0].attributes("href")).toBe(
      "/data-quality/executions/workspaces/ws/qualityExecutions/e-oldest"
    );
  });

  it("names each square with its verdict and absolute finish time", async () => {
    const newest = execution("e-newest", QualityStatus.ERROR, 2);
    const wrapper = await mountStrip({ executions: [newest] });

    const square = squares(wrapper)[0];
    expect(square.attributes("title")).toBe(
      `${i18n.global.t("dataQuality.status.error")} · ${formatDateTime(newest.finishTime, LOCALE)}`
    );
    expect(square.attributes("aria-label")).toBe(
      `${i18n.global.t("dataQuality.status.error")} · ${formatDateTime(newest.finishTime, LOCALE)}`
    );
  });

  it("colors each verdict with its theme token", async () => {
    const cases: Array<[QualityStatus, string]> = [
      [QualityStatus.PASS, "hsl(var(--success))"],
      [QualityStatus.WARN, "hsl(var(--warning))"],
      [QualityStatus.FAIL, "hsl(var(--destructive))"],
      [QualityStatus.ERROR, "hsl(var(--danger))"],
      [QualityStatus.SKIPPED, "hsl(var(--muted-foreground) / 0.35)"],
      [QualityStatus.NOT_EVALUATED, "hsl(var(--border))"],
    ];
    for (const [status, color] of cases) {
      const wrapper = await mountStrip({
        executions: [execution(`e-${status}`, status, 0)],
      });
      expect(squares(wrapper)[0].attributes("style")).toContain(
        `background-color: ${color}`
      );
      wrapper.unmount();
    }
  });

  it("renders nothing when there are no executions", async () => {
    const wrapper = await mountStrip({ executions: [] });

    expect(wrapper.find('[role="list"]').exists()).toBe(false);
    expect(squares(wrapper)).toHaveLength(0);
  });
});
