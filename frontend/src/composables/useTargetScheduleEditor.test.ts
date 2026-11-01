import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { computed, defineComponent, ref } from "vue";
import { i18n } from "@/locales";
import {
  QualityTargetScheduleSchema,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { useTargetScheduleEditor } from "./useTargetScheduleEditor";

const mocks = vi.hoisted(() => ({
  updateQualityTargetSchedule: vi.fn(),
  success: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  updateQualityTargetSchedule: mocks.updateQualityTargetSchedule,
}));

vi.mock("@/lib/notify", () => ({
  notify: {
    success: mocks.success,
    info: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
  },
}));

function setup(
  options: {
    /** The nominal gap the aggregate read returns for either schedule form. */
    intervalSeconds?: bigint;
    cron?: string;
    timezone?: string;
  } = {}
) {
  const hasSchedule =
    options.intervalSeconds !== undefined ||
    options.cron !== undefined ||
    options.timezone !== undefined;
  const summary = ref(
    create(QualityTargetSummarySchema, {
      schedule: hasSchedule
        ? create(QualityTargetScheduleSchema, {
            scheduleInterval:
              options.intervalSeconds === undefined
                ? undefined
                : create(DurationSchema, { seconds: options.intervalSeconds }),
            cron: options.cron ?? "",
            timezone: options.timezone ?? "",
          })
        : undefined,
    })
  );
  const Harness = defineComponent({
    setup() {
      return {
        editor: useTargetScheduleEditor({
          summary,
          targetGuid: computed(() => "inst1;shop;public;orders"),
          effectiveMetaType: computed(() => MetaType.TABLE),
        }),
      };
    },
    template: "<div />",
  });
  const wrapper = mount(Harness, { global: { plugins: [i18n] } });
  return {
    summary,
    editor: (
      wrapper.vm as unknown as {
        editor: ReturnType<typeof useTargetScheduleEditor>;
      }
    ).editor,
  };
}

beforeEach(() => {
  mocks.updateQualityTargetSchedule.mockReset();
  mocks.success.mockReset();
});

describe("useTargetScheduleEditor", () => {
  it("seeds the editor from a whole-hour interval", () => {
    const { editor } = setup({ intervalSeconds: 7200n });

    editor.openScheduleEditor();

    expect(editor.scheduleOpen.value).toBe(true);
    expect(editor.scheduleMode.value).toBe("interval");
    expect(editor.scheduleValue.value).toBe("2");
    expect(editor.scheduleUnit.value).toBe("hours");
    expect(editor.scheduleError.value).toBe("");
  });

  it("starts a schedule-less target at one hour", () => {
    const { editor } = setup();

    editor.openScheduleEditor();

    expect(editor.scheduleValue.value).toBe("1");
    expect(editor.scheduleUnit.value).toBe("hours");
    // No stored cron yet: the zone defaults to the reader's own.
    expect(editor.scheduleMode.value).toBe("interval");
    expect(editor.scheduleTimezone.value).not.toBe("");
  });

  it("seeds the cron editor from a cron schedule and keeps the nominal gap", () => {
    const { editor } = setup({
      intervalSeconds: 86_400n,
      cron: "0 2 * * *",
      timezone: "Asia/Shanghai",
    });

    editor.openScheduleEditor();

    expect(editor.scheduleMode.value).toBe("cron");
    expect(editor.cronExpression.value).toBe("0 2 * * *");
    expect(editor.scheduleTimezone.value).toBe("Asia/Shanghai");
    // Switching back to the interval form starts from the expression's gap.
    expect(editor.scheduleValue.value).toBe("1");
    expect(editor.scheduleUnit.value).toBe("days");
  });

  it("offers the stored and browser zones alongside the curated list", () => {
    const { editor } = setup({
      intervalSeconds: 86_400n,
      cron: "0 2 * * *",
      timezone: "Europe/Madrid",
    });

    editor.openScheduleEditor();

    expect(editor.timezoneOptions.value).toContain("UTC");
    expect(editor.timezoneOptions.value).toContain("Europe/Madrid");
    expect(editor.timezoneOptions.value).toContain(
      editor.scheduleTimezone.value
    );
  });

  it("validates the interval as the reader types", () => {
    const { editor } = setup({ intervalSeconds: 7200n });
    editor.openScheduleEditor();

    expect(editor.scheduleIssueKey.value).toBe("");

    editor.scheduleValue.value = "0";
    expect(editor.scheduleIssueKey.value).not.toBe("");

    editor.setScheduleUnit("minutes");
    editor.scheduleValue.value = "20";
    expect(editor.scheduleIssueKey.value).toBe("");
  });

  it("validates both the cron expression and its zone", () => {
    const { editor } = setup();
    editor.openScheduleEditor();
    editor.setScheduleMode("cron");

    editor.cronExpression.value = "0 2 * *";
    expect(editor.scheduleIssueKey.value).toBe(
      "dataQuality.editor.validation.cronInvalid"
    );

    editor.cronExpression.value = "0 2 * * *";
    expect(editor.scheduleIssueKey.value).toBe("");

    editor.setScheduleTimezone("Not/AZone");
    expect(editor.scheduleIssueKey.value).toBe(
      "dataQuality.editor.validation.timezoneInvalid"
    );
  });

  it("does not save an invalid interval", async () => {
    const { editor } = setup();
    editor.openScheduleEditor();
    editor.scheduleValue.value = "0";

    await editor.saveSchedule();

    expect(mocks.updateQualityTargetSchedule).not.toHaveBeenCalled();
  });

  it("does not save an invalid cron expression", async () => {
    const { editor } = setup();
    editor.openScheduleEditor();
    editor.setScheduleMode("cron");
    editor.cronExpression.value = "0 2 * *";

    await editor.saveSchedule();

    expect(mocks.updateQualityTargetSchedule).not.toHaveBeenCalled();
  });

  it("saves the interval and patches the summary with the server's answer", async () => {
    const updated = create(QualityTargetScheduleSchema, {
      scheduleInterval: create(DurationSchema, { seconds: 10800n }),
    });
    mocks.updateQualityTargetSchedule.mockResolvedValue(updated);
    const { summary, editor } = setup();
    editor.openScheduleEditor();
    editor.scheduleValue.value = "3";

    await editor.saveSchedule();

    expect(mocks.updateQualityTargetSchedule).toHaveBeenCalledWith({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
      scheduleInterval: expect.objectContaining({ seconds: 10800n }),
    });
    expect(summary.value.schedule?.scheduleInterval?.seconds).toBe(10800n);
    expect(editor.scheduleOpen.value).toBe(false);
    expect(editor.scheduleSaving.value).toBe(false);
    expect(mocks.success).toHaveBeenCalledOnce();
  });

  it("sends only the cron form when that mode is chosen", async () => {
    const updated = create(QualityTargetScheduleSchema, {
      scheduleInterval: create(DurationSchema, { seconds: 86_400n }),
      cron: "0 2 * * *",
      timezone: "Asia/Shanghai",
    });
    mocks.updateQualityTargetSchedule.mockResolvedValue(updated);
    const { summary, editor } = setup();
    editor.openScheduleEditor();
    editor.setScheduleMode("cron");
    editor.cronExpression.value = "  0 2 * * *  ";
    editor.setScheduleTimezone("Asia/Shanghai");

    await editor.saveSchedule();

    expect(mocks.updateQualityTargetSchedule).toHaveBeenCalledWith({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
      cron: "0 2 * * *",
      timezone: "Asia/Shanghai",
    });
    expect(summary.value.schedule?.cron).toBe("0 2 * * *");
    expect(editor.scheduleOpen.value).toBe(false);
  });

  it("does not run two saves at once", async () => {
    let release!: () => void;
    const pending = new Promise<never>((resolve) => {
      release = () => resolve(undefined as never);
    });
    mocks.updateQualityTargetSchedule.mockReturnValue(pending);
    const { editor } = setup();
    editor.openScheduleEditor();

    const first = editor.saveSchedule();
    const second = editor.saveSchedule();
    expect(mocks.updateQualityTargetSchedule).toHaveBeenCalledOnce();

    release();
    await Promise.all([first, second]);
  });

  it("keeps the dialog open with the failure's sentence", async () => {
    mocks.updateQualityTargetSchedule.mockRejectedValue(
      new ConnectError("interval too small", Code.FailedPrecondition)
    );
    const { editor } = setup();
    editor.openScheduleEditor();

    await editor.saveSchedule();

    expect(editor.scheduleOpen.value).toBe(true);
    expect(editor.scheduleError.value).toContain("interval too small");
    expect(editor.scheduleSaving.value).toBe(false);
  });

  it("closes through the dialog's open binding", () => {
    const { editor } = setup();
    editor.openScheduleEditor();

    editor.onScheduleOpenChange(false);
    expect(editor.scheduleOpen.value).toBe(false);

    editor.onScheduleOpenChange(true);
    expect(editor.scheduleOpen.value).toBe(false);
  });
});
