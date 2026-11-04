import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  QualityChangeRejectionReason,
  QualityChangeRequestItemKind,
  QualityChangeRequestItemResult_Status,
  QualityChangeRequestItemResultSchema,
  QualityChangeRequestItemSchema,
  QualityChangeRequestSchema,
  QualityChangeRequestState,
  QualityCoverageProperty,
  QualityRuleDraftConfigSchema,
  QualitySuggestionKind,
  QualitySuggestionPayloadSchema,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import ChangeRequestsDialog from "./ChangeRequestsDialog.vue";

const mocks = vi.hoisted(() => ({
  listQualityChangeRequests: vi.fn(),
  listQualitySuggestions: vi.fn(),
  decideQualityChangeRequest: vi.fn(),
}));

const notifyMock = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityChangeRequests: mocks.listQualityChangeRequests,
  listQualitySuggestions: mocks.listQualitySuggestions,
  decideQualityChangeRequest: mocks.decideQualityChangeRequest,
}));

vi.mock("@/lib/notify", () => ({ notify: notifyMock }));

const TABLE = "inst1;db1;public;orders";

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function requestFixture(
  options: {
    state?: QualityChangeRequestState;
    failed?: boolean;
    rejected?: boolean;
  } = {}
) {
  const state = options.state ?? QualityChangeRequestState.PROPOSED;
  return create(QualityChangeRequestSchema, {
    name: "workspaces/ws1/qualityChangeRequests/cr1",
    state,
    baseVersion: 2,
    items: [
      create(QualityChangeRequestItemSchema, {
        kind: QualityChangeRequestItemKind.SUGGESTION_REF,
        suggestion: "workspaces/ws1/qualitySuggestions/s1",
      }),
      create(QualityChangeRequestItemSchema, {
        kind: QualityChangeRequestItemKind.COVERAGE_CHANGE,
        property: QualityCoverageProperty.FRESHNESS,
        minChecks: 1,
      }),
    ],
    itemResults:
      state === QualityChangeRequestState.APPLIED
        ? [
            create(QualityChangeRequestItemResultSchema, {
              index: 0,
              status: options.failed
                ? QualityChangeRequestItemResult_Status.FAILED
                : QualityChangeRequestItemResult_Status.APPLIED,
              detail: options.failed
                ? "the suggestion is not a rule draft"
                : "",
            }),
            create(QualityChangeRequestItemResultSchema, {
              index: 1,
              status: QualityChangeRequestItemResult_Status.APPLIED,
            }),
          ]
        : [],
    rejectionReasons: options.rejected
      ? [QualityChangeRejectionReason.NOISE]
      : [],
    decisionNote: options.rejected ? "too noisy" : "",
  });
}

function mountDialog(options: {
  requests: ReturnType<typeof requestFixture>[];
  canDecide?: boolean;
  canSuggest?: boolean;
}) {
  mocks.listQualityChangeRequests.mockResolvedValue({
    items: options.requests,
    nextPageToken: "",
  });
  return mount(ChangeRequestsDialog, {
    attachTo: document.body,
    props: {
      open: true,
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      canDecide: options.canDecide ?? true,
      canSuggest: options.canSuggest ?? false,
    },
    global: { plugins: [i18n] },
  });
}

function buttonsByText(text: string): HTMLElement[] {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].filter((button) => button.textContent?.trim() === text);
}

function buttonByText(text: string): HTMLElement | undefined {
  return buttonsByText(text)[0];
}

/** The decision panel's confirm reuses the trigger's label; it is the later one. */
function confirmButton(text: string): HTMLElement | undefined {
  const matches = buttonsByText(text);
  return matches[matches.length - 1];
}

describe("ChangeRequestsDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [
        create(QualitySuggestionSchema, {
          name: "workspaces/ws1/qualitySuggestions/s1",
          kind: QualitySuggestionKind.RULE_DRAFT,
          status: QualitySuggestionStatus.PENDING,
          payload: create(QualitySuggestionPayloadSchema, {
            payload: {
              case: "rule",
              value: create(QualityRuleDraftConfigSchema, {
                title: "orders must have rows",
              }),
            },
          }),
        }),
      ],
      nextPageToken: "",
    });
    mocks.decideQualityChangeRequest.mockImplementation(
      async (options: { name: string; decision: number }) =>
        create(QualityChangeRequestSchema, {
          name: options.name,
          state:
            options.decision === 1
              ? QualityChangeRequestState.APPLIED
              : QualityChangeRequestState.REJECTED,
        })
    );
  });

  it("renders each typed item and its result", async () => {
    mountDialog({
      requests: [
        requestFixture({
          state: QualityChangeRequestState.APPLIED,
          failed: true,
        }),
      ],
      canDecide: false,
    });
    await flushPromises();

    const text = document.body.textContent ?? "";
    expect(text).toContain(
      label("dataQuality.changeRequests.setDeclaration", {
        scope: label("dataQuality.changeRequests.wholeTable", {
          property: label("dataQuality.workspace.planPropertyFreshness"),
        }),
        count: 1,
      })
    );
    expect(text).toContain(label("dataQuality.changeRequests.resultFailed"));
    expect(text).toContain(label("dataQuality.changeRequests.stateApplied"));
  });

  it("resolves a suggestion title when the reviewer may read proposals", async () => {
    mountDialog({
      requests: [requestFixture()],
      canSuggest: true,
    });
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.changeRequests.acceptProposal", {
        title: "orders must have rows",
      })
    );
  });

  it("approves a proposed batch through the decision API", async () => {
    const wrapper = mountDialog({ requests: [requestFixture()] });
    await flushPromises();

    buttonByText(label("dataQuality.changeRequests.approve"))?.click();
    await flushPromises();
    buttonByText(label("dataQuality.changeRequests.confirmApprove"))?.click();
    await flushPromises();

    expect(mocks.decideQualityChangeRequest).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "workspaces/ws1/qualityChangeRequests/cr1",
        decision: 1,
      })
    );
    expect(wrapper.emitted("decided")).toBeTruthy();
    expect(notifyMock.success).toHaveBeenCalledWith(
      label("dataQuality.changeRequests.appliedNotice")
    );
  });

  it("requires a taxonomy reason before rejecting", async () => {
    mountDialog({ requests: [requestFixture()] });
    await flushPromises();

    buttonByText(label("dataQuality.changeRequests.reject"))?.click();
    await flushPromises();
    // The form preselects one reason, so clear it to prove the guard.
    const checkedReason = document.body.querySelector<HTMLInputElement>(
      '[role="dialog"] input[type="checkbox"]:checked'
    );
    expect(checkedReason).toBeTruthy();
    checkedReason?.click();
    await flushPromises();
    expect(
      confirmButton(
        label("dataQuality.changeRequests.confirmReject")
      )?.hasAttribute("disabled")
    ).toBe(true);

    checkedReason?.click();
    await flushPromises();
    confirmButton(label("dataQuality.changeRequests.confirmReject"))?.click();
    await flushPromises();

    const call = mocks.decideQualityChangeRequest.mock.calls[0]?.[0] as {
      decision: number;
      rejectionReasons?: QualityChangeRejectionReason[];
    };
    expect(call.decision).toBe(2);
    expect(call.rejectionReasons).toContain(QualityChangeRejectionReason.NOISE);
  });

  it("offers no actions for an expired request", async () => {
    mountDialog({
      requests: [requestFixture({ state: QualityChangeRequestState.EXPIRED })],
    });
    await flushPromises();

    expect(
      buttonByText(label("dataQuality.changeRequests.approve"))
    ).toBeUndefined();
    expect(document.body.textContent).toContain(
      label("dataQuality.changeRequests.expiredHint")
    );
  });

  it("shows the rejection taxonomy a rejected batch recorded", async () => {
    mountDialog({
      requests: [
        requestFixture({
          state: QualityChangeRequestState.REJECTED,
          rejected: true,
        }),
      ],
    });
    await flushPromises();
    expect(document.body.textContent).toContain(
      label("dataQuality.changeRequests.rejectedWith", {
        reasons: label("dataQuality.changeRequests.reasonNoise"),
      })
    );
  });
});
