import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import {
  ColumnProfileSchema,
  ColumnProfileStatsSchema,
  QualitySuggestionQueueSummarySchema,
  RecommendQualityChecksResponseSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import RecommendChecksButton from "./RecommendChecksButton.vue";

const mocks = vi.hoisted(() => ({
  listColumnProfiles: vi.fn(),
  profileColumns: vi.fn(),
  recommendQualityChecks: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listColumnProfiles: mocks.listColumnProfiles,
  profileColumns: mocks.profileColumns,
  recommendQualityChecks: mocks.recommendQualityChecks,
}));

const TARGET = "inst1;db1;public;orders";

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

/** A stored profile for one column, measured `ageDays` ago. */
function profile(column: string, ageDays = 0) {
  return create(ColumnProfileSchema, {
    tableGuid: TARGET,
    metaType: MetaType.TABLE,
    column,
    profile: create(ColumnProfileStatsSchema, { totalRows: 100n }),
    profiledAt: timestampFromDate(
      new Date(Date.now() - ageDays * 24 * 60 * 60 * 1000)
    ),
  });
}

function recommended() {
  return create(RecommendQualityChecksResponseSchema, {
    qualitySuggestions: [],
    summary: create(QualitySuggestionQueueSummarySchema, { created: 2 }),
  });
}

async function mountButton(
  props: Partial<{
    columns: string[];
    canProfile: boolean;
    disabled: boolean;
  }> = {}
) {
  const wrapper = mount(RecommendChecksButton, {
    attachTo: document.body,
    props: {
      targetGuid: TARGET,
      metaType: MetaType.TABLE,
      ...props,
    },
    global: { plugins: [i18n] },
  });
  await flushPromises();
  return wrapper;
}

function primaryButton(): HTMLButtonElement {
  const button = [...document.body.querySelectorAll("button")].find(
    (candidate) =>
      candidate.textContent?.trim() === label("dataQuality.recommend.label")
  );
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error("the recommendation button is missing");
  }
  return button;
}

describe("RecommendChecksButton", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.recommendQualityChecks.mockResolvedValue(recommended());
    mocks.listColumnProfiles.mockResolvedValue([
      profile("email"),
      profile("amount"),
    ]);
    mocks.profileColumns.mockResolvedValue([]);
  });

  it("refuses to recommend against stale profiles without the profile permission", async () => {
    mocks.listColumnProfiles.mockResolvedValue([profile("email", 8)]);

    const wrapper = await mountButton({
      columns: ["email", "amount"],
      canProfile: false,
    });
    primaryButton().click();
    await flushPromises();

    expect(mocks.profileColumns).not.toHaveBeenCalled();
    expect(mocks.recommendQualityChecks).not.toHaveBeenCalled();
    expect(wrapper.emitted("failed")?.[0]).toEqual([
      label("dataQuality.recommend.profileRequired"),
    ]);
    wrapper.unmount();
  });

  it("profiles the stale columns first, then recommends for the target", async () => {
    mocks.listColumnProfiles.mockResolvedValue([
      profile("email"),
      profile("amount", 8),
    ]);
    const success = vi.spyOn(notify, "success");

    const wrapper = await mountButton({
      columns: ["email", "amount"],
      canProfile: true,
    });
    primaryButton().click();
    await flushPromises();

    // Only the stale column is measured again, and the analysis finishes before
    // the recommendation request leaves.
    expect(mocks.profileColumns).toHaveBeenCalledWith({
      tableGuid: TARGET,
      metaType: MetaType.TABLE,
      columns: ["amount"],
    });
    expect(mocks.profileColumns.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.recommendQualityChecks.mock.invocationCallOrder[0] ?? 0
    );
    expect(mocks.recommendQualityChecks).toHaveBeenCalledWith({
      tableGuid: TARGET,
      metaType: MetaType.TABLE,
      columns: ["email", "amount"],
      language: "en-US",
      forceRegenerate: false,
    });
    // The queue summary explains what the pass did; the parent hears the response.
    expect(success).toHaveBeenCalledWith(
      label("dataQuality.suggestionSummary.created", { count: 2 })
    );
    const suggested = wrapper.emitted("suggested");
    expect(suggested).toHaveLength(1);
    expect(suggested?.[0]?.[0]).toMatchObject({ qualitySuggestions: [] });
    success.mockRestore();
    wrapper.unmount();
  });

  it("recommends without profiling when every requested column is fresh", async () => {
    const wrapper = await mountButton({ columns: ["email", "amount"] });
    primaryButton().click();
    await flushPromises();

    expect(mocks.profileColumns).not.toHaveBeenCalled();
    expect(mocks.recommendQualityChecks).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("offers a forced regeneration as the split action's second choice", async () => {
    const wrapper = await mountButton({ columns: ["email"] });

    const trigger = wrapper.find(
      `[aria-label="${label("dataQuality.recommend.regenerate")}"]`
    );
    expect(trigger.exists()).toBe(true);
    await trigger.trigger("click");
    await flushPromises();

    const item = document.body.querySelector<HTMLElement>('[role="menuitem"]');
    expect(item?.textContent).toContain(
      label("dataQuality.recommend.regenerateHint")
    );
    item?.click();
    await flushPromises();

    expect(mocks.recommendQualityChecks).toHaveBeenCalledWith(
      expect.objectContaining({ forceRegenerate: true })
    );
    wrapper.unmount();
  });

  it("reports a failed recommendation through the localized fallback", async () => {
    mocks.recommendQualityChecks.mockRejectedValue(new Error("offline"));

    const wrapper = await mountButton({ columns: ["email"] });
    primaryButton().click();
    await flushPromises();

    expect(wrapper.emitted("failed")?.[0]).toEqual([
      label("dataQuality.recommend.failed"),
    ]);
    wrapper.unmount();
  });
});
