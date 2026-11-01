import {
  type ComputedRef,
  computed,
  getCurrentScope,
  onScopeDispose,
  type Ref,
  ref,
  watch,
} from "vue";
import { listQualityTargets } from "@/api/data-quality";
import { useAuthStore } from "@/store/modules/auth";
import type { QualityTargetSummary } from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  isQualitySignalMetaType,
  qualitySignalKey,
  summarySignalKey,
} from "@/utils/dataQualitySignal";

/**
 * One registry object whose trust signal the caller wants to show.
 *
 * The pair, not the GUID alone: a GUID does not identify a registry row across
 * object types, and the selection request needs both halves.
 */
export interface QualitySignalTarget {
  guid: string;
  metaType: MetaType;
}

/** What the caller can say about the signals it asked for. */
export type QualitySignalState =
  /** Nothing has been asked for yet. */
  | "idle"
  /** A selection read is in flight; unknown rows are not "not monitored" yet. */
  | "loading"
  /** Every requested row was answered; an absent signal means not monitored. */
  | "ready"
  /** The read failed; unknown is unknown, not "not monitored". */
  | "failed"
  /** The caller may not read the rule catalog, so nothing renders at all. */
  | "denied";

/**
 * The most targets one selection request may name. Mirrors the server's cap, so
 * a caller can hand in a whole page without splitting it first.
 */
export const QUALITY_SIGNAL_BATCH_SIZE = 200;

export interface QualitySignals {
  /** The answers by `qualitySignalKey`; a missing entry is an unmonitored row. */
  signals: Ref<Map<string, QualityTargetSummary>>;
  /** The state of the whole read; see `QualitySignalState`. */
  state: Ref<QualitySignalState>;
  /** Whether the caller holds the rule-catalog permission. */
  canView: ComputedRef<boolean>;
  /** One row's answer, or undefined while unknown / not monitored. */
  signalFor: (
    guid: string,
    metaType: MetaType
  ) => QualityTargetSummary | undefined;
  /** Re-read anything still missing, e.g. after a failed page. */
  reload: () => Promise<void>;
}

function chunk<T>(items: readonly T[], size: number): T[][] {
  const batches: T[][] = [];
  for (let start = 0; start < items.length; start += size) {
    batches.push(items.slice(start, start + size));
  }
  return batches;
}

/**
 * The trust signals of the objects a view is showing, read in batches.
 *
 * `ListQualityTargets` answers an exact selection in request order and omits a
 * key with no live rule, so one call covers a whole page and the answer's
 * absence *is* "not monitored". The composable keeps a map keyed by
 * GUID + meta type, fetches only the keys it does not know yet (a "load more"
 * therefore asks for the new rows alone), aborts a superseded read, and reports
 * one state for the set so a cell can tell "not monitored" from "not loaded
 * yet".
 *
 * A caller without `dataQuality.rules.list` never issues a request: the
 * metadata browser is readable without the quality catalog.
 */
export function useQualitySignals(
  source: () => readonly QualitySignalTarget[]
): QualitySignals {
  const authStore = useAuthStore();
  const canView = computed(() =>
    authStore.hasPermission("metaxisdata.dataQuality.rules.list")
  );

  const signals = ref<Map<string, QualityTargetSummary>>(new Map());
  const state = ref<QualitySignalState>("idle");

  /** The deduplicated, supported targets the caller currently shows. */
  const wanted = computed<QualitySignalTarget[]>(() => {
    const seen = new Set<string>();
    const rows: QualitySignalTarget[] = [];
    for (const target of source()) {
      const guid = target.guid.trim();
      if (!guid || !isQualitySignalMetaType(target.metaType)) {
        continue;
      }
      const key = qualitySignalKey(guid, target.metaType);
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      rows.push({ guid, metaType: target.metaType });
    }
    return rows;
  });

  /** A stable identity for the wanted set, so the watcher fires on real changes. */
  const signature = computed(() =>
    wanted.value
      .map((target) => qualitySignalKey(target.guid, target.metaType))
      .join("|")
  );

  let generation = 0;
  let controller: AbortController | undefined;

  function abortInFlight() {
    controller?.abort();
    controller = undefined;
  }

  async function load() {
    if (!canView.value) {
      generation += 1;
      abortInFlight();
      signals.value = new Map();
      state.value = "denied";
      return;
    }

    const wantedRows = wanted.value;
    const wantedKeys = new Set(
      wantedRows.map((target) => qualitySignalKey(target.guid, target.metaType))
    );
    // Keep the answers that still belong to the screen; drop the rest so a long
    // browsing session does not accumulate every page it has seen.
    const kept = new Map(
      [...signals.value].filter(([key]) => wantedKeys.has(key))
    );
    signals.value = kept;

    if (wantedRows.length === 0) {
      generation += 1;
      abortInFlight();
      state.value = "ready";
      return;
    }

    const missing = wantedRows.filter(
      (target) => !kept.has(qualitySignalKey(target.guid, target.metaType))
    );
    if (missing.length === 0) {
      state.value = "ready";
      return;
    }

    generation += 1;
    const current = generation;
    abortInFlight();
    controller = new AbortController();
    const { signal } = controller;
    state.value = "loading";

    try {
      const pages = await Promise.all(
        chunk(missing, QUALITY_SIGNAL_BATCH_SIZE).map((batch) =>
          listQualityTargets({
            targets: batch.map((target) => ({
              targetGuid: target.guid,
              metaType: target.metaType,
            })),
            signal,
          })
        )
      );
      // A superseded read must not overwrite the set the reader is looking at.
      if (current !== generation || signal.aborted) {
        return;
      }
      const next = new Map(kept);
      for (const page of pages) {
        for (const summary of page.items) {
          const key = summarySignalKey(summary);
          if (key) {
            next.set(key, summary);
          }
        }
      }
      signals.value = next;
      state.value = "ready";
    } catch {
      if (current !== generation || signal.aborted) {
        return;
      }
      // Unknown stays unknown: the cells must not claim "not monitored".
      state.value = "failed";
    }
  }

  watch([signature, canView], () => void load(), { immediate: true });

  if (getCurrentScope()) {
    onScopeDispose(() => {
      generation += 1;
      abortInFlight();
    });
  }

  function signalFor(
    guid: string,
    metaType: MetaType
  ): QualityTargetSummary | undefined {
    return signals.value.get(qualitySignalKey(guid.trim(), metaType));
  }

  return { signals, state, canView, signalFor, reload: load };
}
