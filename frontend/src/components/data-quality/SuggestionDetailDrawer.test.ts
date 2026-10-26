import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  QualityRuleConfigSchema,
  QualitySemanticType,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
  QualityTargetSchema,
  RowCountConfigSchema,
  SemanticLabelSuggestionSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import SuggestionDetailDrawer from "./SuggestionDetailDrawer.vue";

// The drawer is a side sheet: Dialog* render into a teleport in production. The
// stubs keep the content in the mounted tree so the assertions see it.
const dialogStubs = {
  Dialog: { template: "<div><slot /></div>" },
  DialogContent: { template: "<div><slot /></div>" },
  DialogHeader: { template: "<div><slot /></div>" },
  DialogTitle: { template: "<div><slot /></div>" },
  DialogDescription: { template: "<div><slot /></div>" },
  DialogFooter: { template: "<div><slot /></div>" },
};

function mountDrawer(status: QualitySuggestionStatus) {
  const suggestion = create(QualitySuggestionSchema, {
    name: "workspaces/ws/qualitySuggestions/s-1",
    kind: QualitySuggestionKind.LABEL,
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;db1;public;users",
      metaType: MetaType.TABLE,
    }),
    payload: {
      payload: {
        case: "label",
        value: create(SemanticLabelSuggestionSchema, {
          column: "email",
          label: QualitySemanticType.EMAIL,
        }),
      },
    },
    status,
    decideTime: timestampFromDate(new Date("2026-10-22T09:00:00Z")),
    decisionNote: "the column already has a semantic label",
  });
  return mount(SuggestionDetailDrawer, {
    props: {
      open: true,
      suggestion,
      canDecide: false,
      canCreateRule: false,
      deciding: false,
      error: "",
      targetLabel: "users",
    },
    global: { plugins: [i18n], stubs: dialogStubs },
  });
}

// SUPERSEDED is the server closing a proposal, not a reviewer decision: it must
// not render "Decided by -" over an empty decider.
describe("SuggestionDetailDrawer superseded proposals", () => {
  it("shows a server closure with its reason instead of a decision", () => {
    const wrapper = mountDrawer(QualitySuggestionStatus.SUPERSEDED);

    expect(wrapper.text()).toContain("Closed by the server");
    expect(wrapper.text()).toContain("the column already has a semantic label");
    expect(wrapper.text()).not.toContain("Decided by");
    expect(wrapper.text()).not.toContain("Decided at");
  });

  it("keeps the decision wording for accepted and rejected proposals", () => {
    const wrapper = mountDrawer(QualitySuggestionStatus.REJECTED);

    expect(wrapper.text()).toContain("Decided by");
    expect(wrapper.text()).not.toContain("Closed by the server");
  });
});

describe("SuggestionDetailDrawer draft actions", () => {
  it("opens the suggestion's target instead of a rule editor", async () => {
    const suggestion = create(QualitySuggestionSchema, {
      name: "workspaces/ws/qualitySuggestions/draft-1",
      kind: QualitySuggestionKind.RULE_DRAFT,
      target: create(QualityTargetSchema, {
        metaGuid: "inst1;db1;public;users",
        metaType: MetaType.TABLE,
      }),
      payload: {
        payload: {
          case: "rule",
          value: {
            title: "Users should have rows",
            confidence: 0.8,
            config: create(QualityRuleConfigSchema, {
              config: {
                case: "rowCount",
                value: create(RowCountConfigSchema, { minRows: 1n }),
              },
            }),
          },
        },
      },
      status: QualitySuggestionStatus.PENDING,
    });
    const wrapper = mount(SuggestionDetailDrawer, {
      props: {
        open: true,
        suggestion,
        canDecide: true,
        canCreateRule: true,
        deciding: false,
        error: "",
        targetLabel: "users",
      },
      global: { plugins: [i18n], stubs: dialogStubs },
    });

    const openTarget = wrapper
      .findAll("button")
      .find(
        (button) =>
          button.text().trim() ===
          i18n.global.t("dataQuality.targets.openWorkspace")
      );
    expect(openTarget).toBeTruthy();
    await openTarget?.trigger("click");

    expect(wrapper.emitted("openTarget")).toHaveLength(1);
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.suggestions.openInEditor")
    );
  });
});
