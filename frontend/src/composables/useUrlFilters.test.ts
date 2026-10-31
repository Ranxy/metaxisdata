import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { defineComponent } from "vue";
import {
  createMemoryHistory,
  createRouter,
  type LocationQuery,
  type Router,
} from "vue-router";
import { useUrlFilters } from "./useUrlFilters";

interface Filters {
  q: string;
  status: string;
}

function parse(query: LocationQuery): Filters {
  return {
    q: typeof query.q === "string" ? query.q : "",
    status: typeof query.status === "string" ? query.status : "-",
  };
}

function serialize(filters: Filters): Record<string, string> {
  const owned: Record<string, string> = {};
  if (filters.q) {
    owned.q = filters.q;
  }
  if (filters.status !== "-") {
    owned.status = filters.status;
  }
  return owned;
}

async function mountHarness(initial: string) {
  const router: Router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/", component: { template: "<div />" } }],
  });
  await router.push(initial);
  await router.isReady();
  const adopted = vi.fn();
  const Harness = defineComponent({
    setup() {
      const url = useUrlFilters<Filters>({
        keys: ["q", "status"],
        parse,
        serialize,
      });
      url.watchExternal(adopted);
      return { url };
    },
    template: "<div />",
  });
  const wrapper = mount(Harness, { global: { plugins: [router] } });
  return {
    router,
    adopted,
    url: (
      wrapper.vm as unknown as {
        url: ReturnType<typeof useUrlFilters<Filters>>;
      }
    ).url,
    wrapper,
  };
}

describe("useUrlFilters", () => {
  it("reads the filters the URL carries", async () => {
    const { url } = await mountHarness("/?q=abc&status=2&keep=1");

    expect(url.read()).toEqual({ q: "abc", status: "2" });
  });

  it("writes only non-defaults and preserves foreign parameters", async () => {
    const { router, url } = await mountHarness("/?q=abc&keep=1");

    url.write({ q: "def", status: "2" });
    await flushPromises();

    expect(router.currentRoute.value.query).toEqual({
      q: "def",
      status: "2",
      keep: "1",
    });
  });

  it("does not navigate when the URL already says the same", async () => {
    const { router, url } = await mountHarness("/?q=abc&keep=1");
    const replace = vi.spyOn(router, "replace");

    url.write({ q: "abc", status: "-" });
    await flushPromises();

    expect(replace).not.toHaveBeenCalled();
  });

  it("reports whether the URL already matches a filter set", async () => {
    const { url } = await mountHarness("/?q=abc&keep=1");

    expect(url.isCurrent({ q: "abc", status: "-" })).toBe(true);
    expect(url.isCurrent({ q: "abc", status: "2" })).toBe(false);
  });

  it("adopts external query changes but not its own write", async () => {
    const { router, adopted, url } = await mountHarness("/?q=abc");

    url.write({ q: "def", status: "-" });
    await flushPromises();
    expect(adopted).not.toHaveBeenCalled();

    await router.replace({ query: { q: "ghi" } });
    await flushPromises();
    expect(adopted).toHaveBeenCalledOnce();
    expect(adopted).toHaveBeenCalledWith({ q: "ghi", status: "-" });

    // The write it just observed is consumed; the next external change is
    // adopted again instead of being compared with a stale write.
    await router.replace({ query: { q: "jkl" } });
    await flushPromises();
    expect(adopted).toHaveBeenCalledTimes(2);
  });

  it("adopts a write that replaced the query before the navigation landed", async () => {
    const { router, adopted, url } = await mountHarness("/?q=abc");

    url.write({ q: "def", status: "2" });
    // A different navigation lands first, as a back/forward would.
    await router.replace({ query: { q: "external" } });
    await flushPromises();

    expect(adopted).toHaveBeenCalledWith({ q: "external", status: "-" });
  });
});
