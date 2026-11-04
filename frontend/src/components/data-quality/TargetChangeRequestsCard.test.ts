import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  QualityChangeRequestSchema,
  QualityChangeRequestState,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetChangeRequestsCard from "./TargetChangeRequestsCard.vue";

const mocks = vi.hoisted(() => ({
  listQualityChangeRequests: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityChangeRequests: mocks.listQualityChangeRequests,
}));

vi.mock("@/composables/useUserDirectory", () => ({
  useUserDirectory: () => ({
    emailOf: (name: string) => name,
    load: vi.fn(),
  }),
}));

const TABLE = "inst1;db1;public;orders";

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function mountCard() {
  return mount(TargetChangeRequestsCard, {
    attachTo: document.body,
    props: {
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
      canDecide: true,
    },
    global: { plugins: [i18n] },
  });
}

function requestFixture(state: QualityChangeRequestState) {
  return create(QualityChangeRequestSchema, {
    name: `workspaces/ws1/qualityChangeRequests/${state}`,
    state,
    baseVersion: 1,
  });
}

describe("TargetChangeRequestsCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    mocks.listQualityChangeRequests.mockImplementation(
      async (options: { state?: QualityChangeRequestState }) => {
        if (options.state === QualityChangeRequestState.PROPOSED) {
          return {
            items: [requestFixture(QualityChangeRequestState.PROPOSED)],
            nextPageToken: "",
          };
        }
        if (options.state === QualityChangeRequestState.APPROVED) {
          return { items: [], nextPageToken: "" };
        }
        return {
          items: [
            requestFixture(QualityChangeRequestState.PROPOSED),
            requestFixture(QualityChangeRequestState.EXPIRED),
          ],
          nextPageToken: "",
        };
      }
    );
  });

  it("counts the batches waiting for a decision", async () => {
    const wrapper = mountCard();
    await flushPromises();

    expect(wrapper.text()).toContain(
      label("dataQuality.changeRequests.pending", { count: 1 })
    );
    expect(wrapper.text()).toContain(
      label("dataQuality.changeRequests.review")
    );
    wrapper.unmount();
  });

  it("says so when the target has no change requests", async () => {
    mocks.listQualityChangeRequests.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    const wrapper = mountCard();
    await flushPromises();

    expect(wrapper.text()).toContain(label("dataQuality.changeRequests.empty"));
    expect(wrapper.text()).toContain(
      label("dataQuality.changeRequests.pending", { count: 0 })
    );
    wrapper.unmount();
  });
});
