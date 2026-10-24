import { useI18n } from "vue-i18n";
import { notify } from "@/lib/notify";
import type { QualitySuggestionQueueSummary } from "@/types/proto-es/v1/data_quality_service_pb";
import {
  type SuggestionSummarySubject,
  suggestionSummaryReason,
  suggestionSummaryView,
} from "@/utils/dataQualitySuggestion";

/**
 * The one sentence a generation pass's queue summary turns into.
 *
 * A pass reconciles proposals with facts and the live queue, so "nothing was
 * queued" is a normal outcome that the toast explains (the fact exists, the
 * proposal is already pending, it was already decided) instead of leaving the
 * reader with a bare count of zero. Callers pass how many proposals the
 * response carried for the one case the summary cannot answer: an older server
 * that reports none.
 */
/** How many skipped column names one toast names before summarizing the rest. */
const SUMMARY_COLUMN_LIMIT = 6;

export function useSuggestionSummary() {
  const { t } = useI18n();

  /** The columns a label pass left out, named instead of only counted. */
  function skippedColumnsText(
    summary: QualitySuggestionQueueSummary | undefined
  ): string {
    const columns = summary?.skippedColumns ?? [];
    if (columns.length === 0) {
      return "";
    }
    const shown = columns.slice(0, SUMMARY_COLUMN_LIMIT).join(", ");
    const remaining = columns.length - SUMMARY_COLUMN_LIMIT;
    return remaining > 0
      ? t("dataQuality.suggestionSummary.skippedColumnsOverflow", {
          columns: shown,
          count: remaining,
        })
      : t("dataQuality.suggestionSummary.skippedColumns", { columns: shown });
  }

  /** The pass's counts, or why it produced nothing, plus the columns it left out. */
  function summaryText(
    summary: QualitySuggestionQueueSummary | undefined,
    subject: SuggestionSummarySubject,
    fallbackCount = 0
  ): string {
    const view = suggestionSummaryView(summary);
    let message: string;
    if (!view.available) {
      message =
        fallbackCount > 0
          ? t("dataQuality.suggestionSummary.queued", { count: fallbackCount })
          : t("dataQuality.suggestionSummary.emptyGeneric");
    } else if (view.queued) {
      message = view.counts
        .map((count) => t(count.key, { count: count.count }))
        .join(" · ");
    } else {
      const reason = suggestionSummaryReason(summary, subject);
      message = t(reason.key, { count: reason.count });
    }
    const skipped = skippedColumnsText(summary);
    return skipped ? `${message} · ${skipped}` : message;
  }

  /** Success when the pass queued something, an explanation when it did not. */
  function notifySummary(
    summary: QualitySuggestionQueueSummary | undefined,
    subject: SuggestionSummarySubject,
    fallbackCount = 0
  ): void {
    const view = suggestionSummaryView(summary);
    const message = summaryText(summary, subject, fallbackCount);
    // An older server reports no summary; the response's own count is then the
    // only evidence the pass queued something.
    if (view.queued || (!view.available && fallbackCount > 0)) {
      notify.success(message);
    } else {
      notify.info(message);
    }
  }

  return { summaryText, notifySummary };
}
