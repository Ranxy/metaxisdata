import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import { defineComponent } from "vue";
import { useBuildInfo } from "./useBuildInfo";

/** A component whose only job is to mount the composable. */
const Host = defineComponent({
  setup() {
    return useBuildInfo();
  },
  render: () => null,
});

function mockFetch(response: Partial<Response> | Error) {
  const fetchMock = vi.fn(() => {
    if (response instanceof Error) {
      return Promise.reject(response);
    }
    return Promise.resolve(response as Response);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("useBuildInfo", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("reads the server's build metadata from /api/version", async () => {
    const info = {
      version: "v1.2.3",
      git_commit: "abc1234",
      build_time: "2025-10-09T00:00:00Z",
    };
    const fetchMock = mockFetch({
      ok: true,
      json: () => Promise.resolve(info),
    } as Partial<Response>);

    const wrapper = mount(Host);
    await flushPromises();

    expect(fetchMock).toHaveBeenCalledWith("/api/version");
    expect(wrapper.vm.buildInfo).toEqual(info);
  });

  // An older server (or a proxy that only forwards /v1) answers the route with
  // something else entirely; the metadata must stay absent rather than throw.
  it("leaves the metadata empty when the route does not answer", async () => {
    mockFetch({ ok: false } as Partial<Response>);

    const wrapper = mount(Host);
    await flushPromises();

    expect(wrapper.vm.buildInfo).toBeNull();
  });

  it("leaves the metadata empty when the request fails", async () => {
    mockFetch(new Error("network down"));

    const wrapper = mount(Host);
    await flushPromises();

    expect(wrapper.vm.buildInfo).toBeNull();
  });
});
