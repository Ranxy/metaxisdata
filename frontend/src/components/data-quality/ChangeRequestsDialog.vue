<template>
  <Dialog
    :open="open"
    @update:open="onOpenChange"
  >
    <DialogContent class="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-3xl">
      <div class="flex max-h-[90vh] min-h-0 flex-col">
        <DialogHeader class="shrink-0 border-b px-6 py-5">
          <DialogTitle>
            {{ t("dataQuality.changeRequests.reviewTitle") }}
          </DialogTitle>
          <DialogDescription>
            {{ t("dataQuality.changeRequests.reviewDescription") }}
          </DialogDescription>
        </DialogHeader>

        <div class="min-h-0 flex-1 space-y-3 overflow-auto px-6 py-5">
          <Alert
            v-if="error"
            variant="destructive"
          >
            <AlertTriangle class="h-4 w-4" />
            <AlertTitle>{{ t("dataQuality.changeRequests.loadFailed") }}</AlertTitle>
            <AlertDescription class="break-words">
              {{ error }}
            </AlertDescription>
          </Alert>

          <div
            v-if="loading"
            class="flex items-center gap-2 text-sm text-muted-foreground"
          >
            <Loader2 class="h-4 w-4 animate-spin" />
          </div>
          <p
            v-else-if="!error && requests.length === 0"
            class="text-sm text-muted-foreground"
          >
            {{ t("dataQuality.changeRequests.empty") }}
          </p>

          <section
            v-for="request in requests"
            :key="request.name"
            class="space-y-2 rounded-md border p-3"
            :data-testid="`change-request-${request.name}`"
          >
            <div class="flex flex-wrap items-center gap-2">
              <Badge :variant="stateVariant(request.state)">
                {{ stateLabel(request) }}
              </Badge>
              <!-- Who proposed the batch matters: an agent's request is a proposal,
                   a person's is their own; the decision stays human either way. -->
              <Badge
                v-if="request.source === QualityChangeRequestSource.AGENT"
                variant="info"
              >
                {{ t("dataQuality.changeRequests.sourceAgent") }}
              </Badge>
              <Badge
                v-else-if="request.source === QualityChangeRequestSource.HUMAN"
                variant="neutral"
              >
                {{ t("dataQuality.changeRequests.sourceHuman") }}
              </Badge>
              <span class="text-xs text-muted-foreground">
                {{
                  t("dataQuality.changeRequests.baseVersion", {
                    version: request.baseVersion,
                  })
                }}
              </span>
              <span
                v-if="request.createdBy"
                class="text-xs text-muted-foreground"
                :title="request.createdBy"
              >
                {{
                  t("dataQuality.changeRequests.createdBy", {
                    name: creatorLabel(request.createdBy),
                  })
                }}
              </span>
              <span class="text-xs text-muted-foreground">
                {{ t("dataQuality.changeRequests.itemCount", { count: request.items.length }) }}
              </span>
            </div>

            <ul class="space-y-1">
              <li
                v-for="(item, index) in request.items"
                :key="index"
                class="flex flex-wrap items-center justify-between gap-2 text-sm"
              >
                <span>{{ itemLabel(item) }}</span>
                <span class="flex flex-wrap items-center gap-2">
                  <span
                    v-if="resultDetail(request, index)"
                    class="text-xs text-muted-foreground"
                  >
                    {{ resultDetail(request, index) }}
                  </span>
                  <Badge
                    v-if="resultOf(request, index)"
                    :variant="resultVariant(resultOf(request, index)?.status)"
                  >
                    {{ resultLabel(resultOf(request, index)?.status) }}
                  </Badge>
                </span>
              </li>
            </ul>

            <div
              v-if="request.state === QualityChangeRequestState.REJECTED"
              class="text-xs text-muted-foreground"
            >
              {{
                t("dataQuality.changeRequests.rejectedWith", {
                  reasons: rejectionLabels(request).join(", "),
                })
              }}
              <span v-if="request.decisionNote"> — {{ request.decisionNote }}</span>
            </div>
            <p
              v-else-if="request.state === QualityChangeRequestState.EXPIRED"
              class="text-xs text-muted-foreground"
            >
              {{ t("dataQuality.changeRequests.expiredHint") }}
            </p>
            <p
              v-else-if="request.state === QualityChangeRequestState.APPROVED"
              class="text-xs text-muted-foreground"
            >
              {{ t("dataQuality.changeRequests.approvedHint") }}
            </p>
            <p
              v-if="request.state !== QualityChangeRequestState.PROPOSED && request.decisionNote"
              class="text-xs text-muted-foreground"
            >
              {{ t("dataQuality.changeRequests.decisionNote", { note: request.decisionNote }) }}
            </p>

            <div
              v-if="canDecide && isDecidable(request)"
              class="flex flex-wrap items-center gap-2"
            >
              <Button
                size="sm"
                :disabled="busy === request.name"
                @click="beginDecision(request, 'approve')"
              >
                {{ t("dataQuality.changeRequests.approve") }}
              </Button>
              <Button
                v-if="canReject(request)"
                size="sm"
                variant="outline"
                :disabled="busy === request.name"
                @click="beginDecision(request, 'reject')"
              >
                {{ t("dataQuality.changeRequests.reject") }}
              </Button>
            </div>

            <!-- One request's decision panel at a time keeps the form honest: the
                 note and the reasons apply to the batch being decided. -->
            <div
              v-if="decision?.name === request.name"
              class="space-y-2 rounded-md border border-dashed p-2"
            >
              <Label :for="`dq-change-request-note-${request.name}`">
                {{ t("dataQuality.changeRequests.noteLabel") }}
              </Label>
              <textarea
                :id="`dq-change-request-note-${request.name}`"
                v-model="decision.note"
                maxlength="500"
                class="min-h-16 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                :placeholder="t('dataQuality.changeRequests.notePlaceholder')"
              />
              <div
                v-if="decision.mode === 'reject'"
                role="group"
                :aria-labelledby="`dq-change-request-reasons-${request.name}`"
                class="space-y-1"
              >
                <p
                  :id="`dq-change-request-reasons-${request.name}`"
                  class="text-sm font-medium"
                >
                  {{ t("dataQuality.changeRequests.reasonsLabel") }}
                </p>
                <div class="grid gap-1 sm:grid-cols-2">
                <label
                  v-for="reason in rejectionOptions"
                  :key="reason.value"
                  class="flex items-center gap-2 text-sm"
                >
                  <input
                    type="checkbox"
                    :checked="decision.reasons.includes(reason.value)"
                    @change="toggleReason(reason.value)"
                  />
                  {{ t(reason.key) }}
                </label>
                </div>
              </div>
              <label
                v-if="decision.mode === 'reject'"
                class="flex items-start gap-2 text-sm"
              >
                <input
                  type="checkbox"
                  class="mt-0.5"
                  :checked="decision.remember"
                  @change="decision.remember = !decision.remember"
                />
                <span>
                  {{ t("dataQuality.changeRequests.rememberReason") }}
                  <span class="block text-xs text-muted-foreground">
                    {{ t("dataQuality.changeRequests.rememberReasonHint") }}
                  </span>
                </span>
              </label>
              <p
                v-if="decision.mode === 'reject' && decision.reasons.length === 0"
                class="text-xs text-muted-foreground"
              >
                {{ t("dataQuality.changeRequests.reasonRequired") }}
              </p>
              <Alert
                v-if="decisionError"
                variant="destructive"
              >
                <AlertTriangle class="h-4 w-4" />
                <AlertDescription class="break-words">
                  {{ decisionError }}
                </AlertDescription>
              </Alert>
              <div class="flex flex-wrap items-center gap-2">
                <Button
                  size="sm"
                  :disabled="busy === request.name || !canSubmitDecision"
                  @click="submitDecision(request)"
                >
                  {{
                    decision.mode === "approve"
                      ? t("dataQuality.changeRequests.confirmApprove")
                      : t("dataQuality.changeRequests.confirmReject")
                  }}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  :disabled="busy === request.name"
                  @click="cancelDecision"
                >
                  {{ t("common.cancel") }}
                </Button>
              </div>
            </div>
          </section>
        </div>

        <DialogFooter class="shrink-0 border-t px-6 py-4">
          <Button
            v-if="nextPageToken"
            variant="ghost"
            :disabled="loadingMore"
            @click="load(true)"
          >
            {{ t("dataQuality.changeRequests.loadMore") }}
          </Button>
          <Button
            variant="outline"
            @click="close"
          >
            {{ t("dataQuality.changeRequests.close") }}
          </Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>
</template>

<script setup lang="ts">
import { Code, ConnectError } from "@connectrpc/connect";
import { AlertTriangle, Loader2 } from "lucide-vue-next";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  createQualityMemory,
  decideQualityChangeRequest,
  listQualityChangeRequests,
  listQualitySuggestions,
} from "@/api/data-quality";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { useErrorMessage } from "@/composables/useErrorHandler";
import { useUserDirectory } from "@/composables/useUserDirectory";
import { notify } from "@/lib/notify";
import {
  DecideQualityChangeRequestRequest_Decision,
  QualityChangeRejectionReason,
  type QualityChangeRequest,
  QualityChangeRequestItemKind,
  QualityChangeRequestItemResult_Status,
  QualityChangeRequestSource,
  QualityChangeRequestState,
  QualityMemoryKind,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityCoveragePropertyKey } from "@/utils/dataQuality";

/**
 * The review surface for a target's change requests. Approval applies each item
 * through the server's existing paths and returns a result per item; a failed item
 * leaves the request approved so it can be retried. Rejection records the structured
 * reason, which is the part later decisions learn from.
 */

const props = withDefaults(
  defineProps<{
    open: boolean;
    targetGuid: string;
    canDecide?: boolean;
    canSuggest?: boolean;
  }>(),
  { canDecide: false, canSuggest: false }
);

const emit = defineEmits<{
  "update:open": [value: boolean];
  decided: [];
}>();

interface DecisionDraft {
  name: string;
  mode: "approve" | "reject";
  note: string;
  reasons: QualityChangeRejectionReason[];
  /** Whether a rejection also records why as durable decision memory. */
  remember: boolean;
}

const { t } = useI18n();
const { formatError } = useErrorMessage();
const { emailOf, load: loadDirectory } = useUserDirectory();

const requests = ref<QualityChangeRequest[]>([]);
const suggestionTitles = ref<Record<string, string>>({});
const loading = ref(false);
const loadingMore = ref(false);
const error = ref("");
const busy = ref("");
const decision = ref<DecisionDraft | null>(null);
const decisionError = ref("");
const nextPageToken = ref("");
let generation = 0;
let controller: AbortController | null = null;

const rejectionOptions = [
  QualityChangeRejectionReason.NOT_IMPORTANT,
  QualityChangeRejectionReason.TOO_TIGHT,
  QualityChangeRejectionReason.NOISE,
  QualityChangeRejectionReason.COVERED_UPSTREAM,
  QualityChangeRejectionReason.WRONG_COLUMN,
  QualityChangeRejectionReason.DEFER,
  QualityChangeRejectionReason.OTHER,
].map((value) => ({ value, key: rejectionKey(value) }));

function rejectionKey(reason: QualityChangeRejectionReason): string {
  switch (reason) {
    case QualityChangeRejectionReason.NOT_IMPORTANT:
      return "dataQuality.changeRequests.reasonNotImportant";
    case QualityChangeRejectionReason.TOO_TIGHT:
      return "dataQuality.changeRequests.reasonTooTight";
    case QualityChangeRejectionReason.NOISE:
      return "dataQuality.changeRequests.reasonNoise";
    case QualityChangeRejectionReason.COVERED_UPSTREAM:
      return "dataQuality.changeRequests.reasonCoveredUpstream";
    case QualityChangeRejectionReason.WRONG_COLUMN:
      return "dataQuality.changeRequests.reasonWrongColumn";
    case QualityChangeRejectionReason.DEFER:
      return "dataQuality.changeRequests.reasonDefer";
    case QualityChangeRejectionReason.OTHER:
      return "dataQuality.changeRequests.reasonOther";
    default:
      return "dataQuality.changeRequests.reasonUnspecified";
  }
}

const canSubmitDecision = computed(
  () =>
    decision.value !== null &&
    (decision.value.mode === "approve" || decision.value.reasons.length > 0)
);

function onOpenChange(value: boolean) {
  if (!value) {
    close();
  }
}

function close() {
  emit("update:open", false);
}

function stateVariant(state: QualityChangeRequestState) {
  switch (state) {
    case QualityChangeRequestState.APPLIED:
      return "success";
    case QualityChangeRequestState.REJECTED:
    case QualityChangeRequestState.EXPIRED:
      return "neutral";
    case QualityChangeRequestState.APPROVED:
      return "warning";
    default:
      return "secondary";
  }
}

function stateLabel(request: QualityChangeRequest): string {
  switch (request.state) {
    case QualityChangeRequestState.PROPOSED:
      return t("dataQuality.changeRequests.stateProposed");
    case QualityChangeRequestState.APPROVED:
      return requestStateHasApplied(request.itemResults)
        ? t("dataQuality.changeRequests.stateApproved")
        : t("dataQuality.changeRequests.stateRetry");
    case QualityChangeRequestState.APPLIED:
      return t("dataQuality.changeRequests.stateApplied");
    case QualityChangeRequestState.REJECTED:
      return t("dataQuality.changeRequests.stateRejected");
    case QualityChangeRequestState.EXPIRED:
      return t("dataQuality.changeRequests.stateExpired");
    default:
      return t("dataQuality.changeRequests.stateUnspecified");
  }
}

function requestStateHasApplied(
  results: QualityChangeRequest["itemResults"]
): boolean {
  return results.some(
    (result) => result.status === QualityChangeRequestItemResult_Status.APPLIED
  );
}

function resultDetail(request: QualityChangeRequest, index: number): string {
  const result = resultOf(request, index);
  if (
    !result ||
    (result.status !== QualityChangeRequestItemResult_Status.FAILED &&
      result.status !== QualityChangeRequestItemResult_Status.SKIPPED)
  ) {
    return "";
  }
  return result.detail;
}

function creatorLabel(name: string): string {
  return emailOf(name) || name;
}

function isDecidable(request: QualityChangeRequest): boolean {
  return (
    request.state === QualityChangeRequestState.PROPOSED ||
    request.state === QualityChangeRequestState.APPROVED
  );
}

/** A request whose items already applied cannot be rejected; one that all failed can. */
function canReject(request: QualityChangeRequest): boolean {
  if (request.state === QualityChangeRequestState.PROPOSED) {
    return true;
  }
  return !request.itemResults.some(
    (result) => result.status === QualityChangeRequestItemResult_Status.APPLIED
  );
}

function resultOf(
  request: QualityChangeRequest,
  index: number
): QualityChangeRequest["itemResults"][number] | undefined {
  return request.itemResults.find((result) => result.index === index);
}

function resultLabel(status?: QualityChangeRequestItemResult_Status): string {
  switch (status) {
    case QualityChangeRequestItemResult_Status.APPLIED:
      return t("dataQuality.changeRequests.resultApplied");
    case QualityChangeRequestItemResult_Status.FAILED:
      return t("dataQuality.changeRequests.resultFailed");
    case QualityChangeRequestItemResult_Status.SKIPPED:
      return t("dataQuality.changeRequests.resultSkipped");
    default:
      return "";
  }
}

function resultVariant(status?: QualityChangeRequestItemResult_Status) {
  switch (status) {
    case QualityChangeRequestItemResult_Status.APPLIED:
      return "success";
    case QualityChangeRequestItemResult_Status.FAILED:
      return "warning";
    default:
      return "neutral";
  }
}

function coverageScope(item: {
  property: QualityChangeRequest["items"][number]["property"];
  column: string;
}): string {
  const property = t(qualityCoveragePropertyKey(item.property));
  return item.column
    ? `${property} · ${item.column}`
    : t("dataQuality.changeRequests.wholeTable", { property });
}

function itemLabel(item: QualityChangeRequest["items"][number]): string {
  switch (item.kind) {
    case QualityChangeRequestItemKind.SUGGESTION_REF: {
      const title = suggestionTitles.value[item.suggestion];
      return title
        ? t("dataQuality.changeRequests.acceptProposal", { title })
        : t("dataQuality.changeRequests.acceptProposalId", {
            suggestion: item.suggestion,
          });
    }
    case QualityChangeRequestItemKind.COVERAGE_CHANGE:
      if (item.remove) {
        return t("dataQuality.changeRequests.removeDeclaration", {
          scope: coverageScope(item),
        });
      }
      return t("dataQuality.changeRequests.setDeclaration", {
        scope: coverageScope(item),
        count: item.minChecks || 1,
      });
    default:
      return t("dataQuality.changeRequests.unknownItem");
  }
}

function rejectionLabels(request: QualityChangeRequest): string[] {
  return request.rejectionReasons.map((reason) => t(rejectionKey(reason)));
}

function beginDecision(
  request: QualityChangeRequest,
  mode: "approve" | "reject"
) {
  decisionError.value = "";
  decision.value = {
    name: request.name,
    mode,
    note: "",
    reasons: mode === "reject" ? [QualityChangeRejectionReason.NOISE] : [],
    remember: mode === "reject",
  };
}

function cancelDecision() {
  decision.value = null;
  decisionError.value = "";
}

function toggleReason(reason: QualityChangeRejectionReason) {
  if (!decision.value) {
    return;
  }
  const reasons = decision.value.reasons;
  const index = reasons.indexOf(reason);
  if (index >= 0) {
    reasons.splice(index, 1);
  } else {
    reasons.push(reason);
  }
}

/**
 * A rejection already carries structured reasons; recording them as a decision
 * memory is what lets later proposals learn from it. The decision has succeeded, so
 * a failed memory write is a warning, never an error on the decision.
 */
async function rememberRejection(
  request: QualityChangeRequest,
  draft: DecisionDraft
) {
  const reasons = request.rejectionReasons.map((reason) =>
    t(rejectionKey(reason))
  );
  const note = draft.note.trim();
  const statement = [reasons.join(", "), note].filter(Boolean).join(" — ");
  try {
    await createQualityMemory({
      kind: QualityMemoryKind.DECISION,
      subject: props.targetGuid,
      statement,
      evidence: [{ kind: "CHANGE_REQUEST", reference: request.name }],
      signal: controller?.signal,
    });
    notify.success(t("dataQuality.changeRequests.memoryRecorded"));
  } catch (e: unknown) {
    notify.warning(
      formatError(e, "dataQuality.changeRequests.memoryRecordFailed")
    );
  }
}

async function submitDecision(request: QualityChangeRequest) {
  const draft = decision.value;
  if (!draft || draft.name !== request.name) {
    return;
  }
  busy.value = request.name;
  decisionError.value = "";
  try {
    const updated = await decideQualityChangeRequest({
      name: request.name,
      decision:
        draft.mode === "approve"
          ? DecideQualityChangeRequestRequest_Decision.APPROVE
          : DecideQualityChangeRequestRequest_Decision.REJECT,
      rejectionReasons: draft.reasons,
      decisionNote: draft.note.trim(),
      signal: controller?.signal,
    });
    if (updated.state === QualityChangeRequestState.APPLIED) {
      notify.success(t("dataQuality.changeRequests.appliedNotice"));
    } else if (updated.state === QualityChangeRequestState.REJECTED) {
      notify.success(t("dataQuality.changeRequests.rejectedNotice"));
    } else if (updated.state === QualityChangeRequestState.EXPIRED) {
      notify.warning(t("dataQuality.changeRequests.expiredNotice"));
    } else {
      notify.warning(t("dataQuality.changeRequests.decidedPartialNotice"));
    }
    if (
      draft.mode === "reject" &&
      draft.remember &&
      updated.state === QualityChangeRequestState.REJECTED
    ) {
      await rememberRejection(updated, draft);
    }
    decision.value = null;
    emit("decided");
    await refresh();
  } catch (e: unknown) {
    if (e instanceof ConnectError && e.code === Code.Aborted) {
      // Another reviewer decided first; reload so the panel stops offering an
      // action that can no longer succeed, and say why above the list.
      decision.value = null;
      await refresh();
      error.value = t("dataQuality.changeRequests.decideStale");
    } else {
      decisionError.value = formatError(
        e,
        "dataQuality.changeRequests.decideFailed"
      );
    }
  } finally {
    busy.value = "";
  }
}

/** Suggestion titles need the suggest permission; without it items show their id. */
async function loadSuggestionTitles(signal: AbortSignal) {
  if (!props.canSuggest) {
    return;
  }
  const guid = props.targetGuid;
  try {
    const page = await listQualitySuggestions({
      targetGuid: guid,
      pageSize: 200,
      signal,
    });
    if (signal.aborted || guid !== props.targetGuid) {
      return;
    }
    const titles: Record<string, string> = {};
    for (const suggestion of page.items) {
      const payload = suggestion.payload?.payload;
      if (payload?.case === "rule") {
        titles[suggestion.name] = payload.value.title;
      }
    }
    suggestionTitles.value = titles;
  } catch {
    if (!signal.aborted) {
      suggestionTitles.value = {};
    }
  }
}

async function load(more = false) {
  const guid = props.targetGuid;
  if (more) {
    loadingMore.value = true;
  } else {
    generation++;
    loading.value = true;
    requests.value = [];
    nextPageToken.value = "";
  }
  const ticket = generation;
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  error.value = "";
  try {
    const page = await listQualityChangeRequests({
      targetGuid: guid,
      pageSize: 20,
      pageToken: more ? nextPageToken.value : "",
      signal,
    });
    if (signal.aborted || ticket !== generation || guid !== props.targetGuid) {
      return;
    }
    requests.value = more ? [...requests.value, ...page.items] : page.items;
    nextPageToken.value = page.nextPageToken;
  } catch (e: unknown) {
    if (signal.aborted || ticket !== generation || guid !== props.targetGuid) {
      return;
    }
    error.value = formatError(e, "dataQuality.changeRequests.loadFailed");
    if (!more) {
      requests.value = [];
    }
  } finally {
    if (ticket === generation) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

async function refresh() {
  await Promise.all([
    load(),
    loadSuggestionTitles(controller?.signal ?? new AbortController().signal),
  ]);
}

watch(
  () => props.open,
  (isOpen) => {
    decision.value = null;
    decisionError.value = "";
    error.value = "";
    if (isOpen) {
      void loadDirectory();
      void load();
      const signal = controller?.signal ?? new AbortController().signal;
      void loadSuggestionTitles(signal);
    }
  },
  { immediate: true }
);

watch(
  () => props.targetGuid,
  () => {
    if (props.open) {
      decision.value = null;
      void load();
      const signal = controller?.signal ?? new AbortController().signal;
      void loadSuggestionTitles(signal);
    }
  }
);

onBeforeUnmount(() => {
  controller?.abort();
});
</script>
