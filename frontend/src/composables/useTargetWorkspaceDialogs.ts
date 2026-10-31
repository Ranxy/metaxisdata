import type { ComputedRef, Ref } from "vue";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { notify } from "@/lib/notify";
import type {
  QualityRule,
  QualitySuggestion,
} from "@/types/proto-es/v1/data_quality_service_pb";

export interface TargetWorkspaceDialogsOptions {
  /** The workspace's loaded rules; the query syncs resolve a name against them. */
  rules: Ref<QualityRule[]>;
  /** True while the workspace is still loading; the syncs wait for it. */
  loading: Ref<boolean>;
  /** Whether the reader may create a check. */
  canCreate: ComputedRef<boolean>;
}

/**
 * The target workspace's dialogs and the query keys that open them.
 *
 * The inline composer (create, edit, or prefilled from an AI draft), the check
 * side sheet, and the legacy `?check=` / `?add=1` / `?edit=1` deep links were
 * one state machine spread through the page: six refs, a dozen openers, and
 * three watchers, none of it testable without mounting the whole workspace.
 * This owns that machine; the page keeps only the submit/close handlers that
 * need its loaded rules.
 */
export function useTargetWorkspaceDialogs(
  options: TargetWorkspaceDialogsOptions
) {
  const route = useRoute();
  const router = useRouter();
  const { t } = useI18n();

  /** The inline composer, open on the target this page names. */
  const composerOpen = ref(false);
  /** The rule the composer edits; undefined makes it create instead. */
  const editingRule = ref<QualityRule>();
  /** The section a new check was opened from, which seeds its type. */
  const composerScope = ref<"dataset" | "column">();
  /** The column a column section named, written into the new check's config. */
  const composerInitialColumn = ref("");
  /** The AI draft the composer was opened from; undefined means a blank create. */
  const composerSuggestion = ref<QualitySuggestion>();
  /** The check whose side sheet is open, by full resource name; empty closes it. */
  const selectedCheck = ref("");

  /**
   * Opens the composer for a new check. A section can narrow it: the dataset
   * header starts on a table-level check, a column header on a check for that
   * column.
   */
  function openAddCheck(
    config: { scope?: "dataset" | "column"; column?: string } = {}
  ) {
    editingRule.value = undefined;
    composerSuggestion.value = undefined;
    composerScope.value = config.scope;
    composerInitialColumn.value = config.column ?? "";
    composerOpen.value = true;
  }

  /** The header's New rule starts a blank create; a row can pass a draft instead. */
  function openComposer(suggestion?: QualitySuggestion) {
    if (suggestion) {
      openComposerFromDraft(suggestion);
      return;
    }
    openAddCheck();
  }

  /**
   * A draft's "adjust" opens the same composer prefilled from it, so its typed
   * config is reviewed beside every other check rather than in a separate editor.
   */
  function openComposerFromDraft(suggestion: QualitySuggestion) {
    editingRule.value = undefined;
    composerSuggestion.value = suggestion;
    composerScope.value = undefined;
    composerInitialColumn.value = "";
    composerOpen.value = true;
  }

  /** The row's edit opens the same composer, fixed to that rule. */
  function openEditor(rule: QualityRule) {
    editingRule.value = rule;
    composerSuggestion.value = undefined;
    composerScope.value = undefined;
    composerInitialColumn.value = "";
    composerOpen.value = true;
  }

  /** A row click opens the check's side sheet without leaving the workspace. */
  function openCheck(rule: QualityRule) {
    selectedCheck.value = rule.name;
  }

  /**
   * Removes `check` from the URL while preserving the other query keys. The edit
   * flag is dropped with it: it only names an intent to open the composer on
   * that check, which closing the sheet has already fulfilled or abandoned.
   */
  function removeCheckQuery() {
    if (route.query.check === undefined && route.query.edit === undefined) {
      return;
    }
    const query = { ...route.query };
    delete query.check;
    delete query.edit;
    void router.replace({ query });
  }

  function closeCheck() {
    selectedCheck.value = "";
    removeCheckQuery();
  }

  function onCheckOpenChange(open: boolean) {
    if (!open) {
      closeCheck();
    }
  }

  /**
   * A check named in the URL opens once the rules have loaded; a name this
   * target does not have (a stale link) is dropped silently rather than shown
   * as an error.
   */
  function syncCheckFromQuery() {
    const name = typeof route.query.check === "string" ? route.query.check : "";
    if (name === "" || options.loading.value) {
      return;
    }
    if (options.rules.value.some((rule) => rule.name === name)) {
      selectedCheck.value = name;
      return;
    }
    selectedCheck.value = "";
    removeCheckQuery();
  }

  /**
   * The DQ directory's start-monitoring picker lands here with `add=1`: open the
   * check dialog as soon as the (possibly empty) workspace is ready, then consume
   * the flag so a refresh does not reopen it.
   */
  function syncAddFromQuery() {
    if (options.loading.value || route.query.add !== "1") {
      return;
    }
    // The flag is consumed first, whatever happens next, so a reload never
    // re-triggers it and a `check` in the same URL keeps its own path.
    const query = { ...route.query };
    delete query.add;
    void router.replace({ query });
    if (!options.canCreate.value) {
      notify.info(t("dataQuality.workspace.createPermissionRequired"));
      return;
    }
    if (route.query.check !== undefined) {
      return;
    }
    openComposer();
  }

  // A query-opened check waits for the rules it names; watching the loading flag
  // re-runs the resolution when the workspace arrives.
  watch(
    [() => route.query.check, options.rules, options.loading],
    syncCheckFromQuery,
    {
      immediate: true,
    }
  );
  watch([() => route.query.add, options.loading], syncAddFromQuery, {
    immediate: true,
  });

  /**
   * The old edit URL arrives as `check=<name>&edit=1`: consume the flag, then open
   * the workspace composer on that check instead of the read-only sheet. A name
   * this target does not have is ignored, exactly like a stale `check`.
   */
  function syncEditFromQuery() {
    if (options.loading.value || route.query.edit !== "1") {
      return;
    }
    const query = { ...route.query };
    delete query.edit;
    void router.replace({ query });
    const check =
      typeof route.query.check === "string" ? route.query.check : "";
    const rule = options.rules.value.find((item) => item.name === check);
    if (!rule) {
      return;
    }
    openEditor(rule);
    closeCheck();
  }

  watch(
    [() => route.query.edit, options.rules, options.loading],
    syncEditFromQuery,
    {
      immediate: true,
    }
  );

  return {
    composerOpen,
    editingRule,
    composerScope,
    composerInitialColumn,
    composerSuggestion,
    selectedCheck,
    openAddCheck,
    openComposer,
    openComposerFromDraft,
    openEditor,
    openCheck,
    closeCheck,
    onCheckOpenChange,
  };
}
