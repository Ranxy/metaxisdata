import { describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import router from "@/router";
import { guidToRouteParams, routeParamToGuid } from "@/utils/guid";

// The real route records, minus the auth guard and the page components, so this
// test proves the decode contract against the patterns the app actually ships.
// If a `+` ever becomes a `(.+)` again, the array-push cases below fail.
const GUID_ROUTES = [
  "MetadataDetail",
  "LineageGraph",
  "OpenLineageColumnLineage",
  "ExplainSQLWithGuid",
];

const Dummy = { render: () => null };

const guidRouter = createRouter({
  history: createMemoryHistory(),
  routes: router
    .getRoutes()
    .filter((record) => GUID_ROUTES.includes(String(record.name)))
    .map((record) => ({
      path: record.path,
      name: record.name,
      component: Dummy,
    })),
});

function currentGuid(): string {
  return routeParamToGuid(guidRouter.currentRoute.value.params.guid);
}

describe("guid routes", () => {
  it.each(GUID_ROUTES)("%s round-trips a GUID", async (name) => {
    const guid = "inst;db;public;orders";

    await guidRouter.push({ name, params: { guid: guidToRouteParams(guid) } });

    expect(currentGuid()).toBe(guid);
  });

  it.each(
    GUID_ROUTES
  )("%s keeps a name that contains a slash in one segment", async (name) => {
    const guid = "inst;db;public;order/items";

    await guidRouter.push({ name, params: { guid: guidToRouteParams(guid) } });

    expect(currentGuid()).toBe(guid);
  });

  it("keeps the empty MySQL schema segment", async () => {
    const guid = "inst;db;;orders";

    await guidRouter.push({
      name: "MetadataDetail",
      params: { guid: guidToRouteParams(guid) },
    });

    expect(currentGuid()).toBe(guid);
  });

  it("keeps a percent sign in a name instead of decoding it twice", async () => {
    const guid = "inst;db;public;50%_off";

    await guidRouter.push({
      name: "MetadataDetail",
      params: { guid: guidToRouteParams(guid) },
    });

    expect(currentGuid()).toBe(guid);
  });

  it("still resolves a hand-typed or bookmarked `;` URL", async () => {
    await guidRouter.push("/metadata/inst;db;tbl");

    expect(currentGuid()).toBe("inst;db;tbl");
  });

  it("still resolves a hand-typed `/`-joined URL with a tilde", async () => {
    await guidRouter.push("/lineage/inst/db/~/orders");

    expect(currentGuid()).toBe("inst;db;;orders");
  });

  it("still resolves the old single-segment `%2F` URLs", async () => {
    // Every URL the string-path form used to produce looks like this.
    await guidRouter.push("/metadata/inst%2Fdb%2F~%2Ftbl");

    expect(currentGuid()).toBe("inst;db;;tbl");
  });
});
