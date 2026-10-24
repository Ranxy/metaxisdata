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
export function useSuggestionSummary() {
  const { t } = useI18n();

  /** The pass's counts, or why it produced nothing. */
  function summaryText(
    summary: QualitySuggestionQueueSummary | undefined,
    subject: SuggestionSummarySubject,
    fallbackCount = 0
  ): string {
    const view = suggestionSummaryView(summary);
    if (!view.available) {
      return fallbackCount > 0
        ? t("dataQuality.suggestionSummary.queued", { count: fallbackCount })
        : t("dataQuality.suggestionSummary.emptyGeneric");
    }
    if (view.queued) {
      return view.counts
        .map((count) => t(count.key, { count: count.count }))
        .join(" · ");
    }
    const reason = suggestionSummaryReason(summary, subject);
    return t(reason.key, { count: reason.count });
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
