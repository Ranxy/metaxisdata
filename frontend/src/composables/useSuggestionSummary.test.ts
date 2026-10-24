import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApp } from "vue";
import { i18n } from "@/locales";
import { QualitySuggestionQueueSummarySchema } from "@/types/proto-es/v1/data_quality_service_pb";
import { useSuggestionSummary } from "./useSuggestionSummary";

const mocks = vi.hoisted(() => ({
  success: vi.fn(),
  info: vi.fn(),
}));

vi.mock("@/lib/notify", () => ({ notify: mocks }));

/** The composable calls `useI18n()`, so it has to run inside a setup. */
function inSetup<T>(composable: () => T): T {
  let result!: T;
  const app = createApp({
    setup() {
      result = composable();
      return () => null;
    },
  });
  app.use(i18n);
  app.mount(document.createElement("div"));
  return result;
}

function summary(fields: {
  created?: number;
  refreshed?: number;
  superseded?: number;
  skippedExistingFact?: number;
  skippedPending?: number;
  skippedDecided?: number;
  skippedColumns?: string[];
}) {
  return create(QualitySuggestionQueueSummarySchema, fields);
}

describe("useSuggestionSummary", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    i18n.global.locale.value = "en-US";
  });

  it("renders the non-zero counts one pass produced", () => {
    const { summaryText } = inSetup(useSuggestionSummary);

    expect(
      summaryText(
        summary({ created: 2, refreshed: 1, skippedExistingFact: 3 }),
        "rule"
      )
    ).toBe("2 new · 1 refreshed · 3 skipped");
  });

  it("explains a pass that queued nothing", () => {
    const { summaryText } = inSetup(useSuggestionSummary);

    expect(summaryText(summary({ skippedExistingFact: 2 }), "label")).toBe(
      "Nothing new: 2 column(s) already have a label."
    );
    expect(summaryText(summary({ skippedPending: 1 }), "rule")).toBe(
      "Nothing new: 1 matching proposal(s) are already waiting for review."
    );
    expect(summaryText(summary({ skippedDecided: 4 }), "rule")).toBe(
      "Nothing new: 4 proposal(s) were already reviewed; regenerate to propose them again."
    );
  });

  it("names the columns a label pass left out", () => {
    const { summaryText } = inSetup(useSuggestionSummary);

    expect(
      summaryText(
        summary({
          skippedExistingFact: 2,
          skippedColumns: ["email", "phone"],
        }),
        "label"
      )
    ).toBe(
      "Nothing new: 2 column(s) already have a label. · Skipped columns: email, phone"
    );
  });

  it("summarizes a long skipped-column list instead of naming every column", () => {
    const { summaryText } = inSetup(useSuggestionSummary);

    expect(
      summaryText(
        summary({
          skippedPending: 8,
          skippedColumns: ["a", "b", "c", "d", "e", "f", "g", "h"],
        }),
        "label"
      )
    ).toBe(
      "Nothing new: 8 matching proposal(s) are already waiting for review. · Skipped columns: a, b, c, d, e, f and 2 more"
    );
  });

  it("falls back to the returned count when the server reports no summary", () => {
    const { summaryText } = inSetup(useSuggestionSummary);

    expect(summaryText(undefined, "rule", 3)).toBe("3 suggestion(s) queued");
    expect(summaryText(undefined, "label", 0)).toBe(
      "No new suggestions this time."
    );
  });

  it("toasts a success only when something was queued", () => {
    const { notifySummary } = inSetup(useSuggestionSummary);

    notifySummary(summary({ created: 1 }), "rule", 1);
    expect(mocks.success).toHaveBeenCalledWith("1 new");

    notifySummary(summary({ skippedPending: 2 }), "rule", 0);
    expect(mocks.info).toHaveBeenCalledWith(
      "Nothing new: 2 matching proposal(s) are already waiting for review."
    );

    // An older server's response is judged by the rows it carried.
    notifySummary(undefined, "rule", 2);
    expect(mocks.success).toHaveBeenLastCalledWith("2 suggestion(s) queued");
    notifySummary(undefined, "rule", 0);
    expect(mocks.info).toHaveBeenLastCalledWith(
      "No new suggestions this time."
    );
  });
});
