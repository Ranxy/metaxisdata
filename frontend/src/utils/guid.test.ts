import { describe, expect, it } from "vitest";
import {
  guidSegmentsToRouteParams,
  guidToRouteParams,
  routeParamToGuid,
} from "./guid";

describe("guidToRouteParams", () => {
  it("keeps the instance/database/schema/table segments apart", () => {
    expect(guidToRouteParams("inst;db;public;orders")).toEqual([
      "inst",
      "db",
      "public",
      "orders",
    ]);
  });

  it("encodes the empty MySQL schema as a tilde", () => {
    expect(guidToRouteParams("inst;db;;orders")).toEqual([
      "inst",
      "db",
      "~",
      "orders",
    ]);
  });

  it("keeps a name that contains a slash or a percent in one element", () => {
    expect(guidToRouteParams("inst;db;public;order/items 50%")).toEqual([
      "inst",
      "db",
      "public",
      "order/items 50%",
    ]);
  });

  it("treats a guid without separators as a single element", () => {
    expect(guidToRouteParams("external:mysql:host:db:table")).toEqual([
      "external:mysql:host:db:table",
    ]);
  });
});

describe("guidSegmentsToRouteParams", () => {
  it("matches the joined-GUID form", () => {
    expect(guidSegmentsToRouteParams(["inst", "db", "", "orders"])).toEqual(
      guidToRouteParams("inst;db;;orders")
    );
  });
});

describe("routeParamToGuid", () => {
  it("decodes the array form vue-router returns", () => {
    expect(routeParamToGuid(["inst", "db", "~", "orders"])).toBe(
      "inst;db;;orders"
    );
  });

  it("keeps slashes, semicolons and percent signs inside an element", () => {
    expect(routeParamToGuid(["inst", "db", "public", "order/items 50%"])).toBe(
      "inst;db;public;order/items 50%"
    );
    expect(routeParamToGuid(["inst;db;tbl"])).toBe("inst;db;tbl");
  });

  it("decodes a hand-typed `/`-joined path", () => {
    expect(routeParamToGuid("inst/db/~/orders")).toBe("inst;db;;orders");
    expect(routeParamToGuid("inst/db/public/orders")).toBe(
      "inst;db;public;orders"
    );
  });

  it("decodes a legacy path push that arrives as one part", () => {
    // What `/metadata/inst%2Fdb%2F~%2Ftbl` hands over: the old string form.
    expect(routeParamToGuid(["inst/db/~/orders"])).toBe("inst;db;;orders");
  });

  it("accepts a legacy `/metadata/<;guid>` segment", () => {
    expect(routeParamToGuid(["inst;db;;orders"])).toBe("inst;db;;orders");
    expect(routeParamToGuid("inst;db;;orders")).toBe("inst;db;;orders");
  });

  it("does not decode a second time, so a `%` in a name survives", () => {
    // vue-router already turned "%25" into "%"; decoding again would throw.
    expect(routeParamToGuid("inst/db/public/a%b")).toBe("inst;db;public;a%b");
  });

  it("returns an empty string for a missing param", () => {
    expect(routeParamToGuid(undefined)).toBe("");
    expect(routeParamToGuid(null)).toBe("");
    expect(routeParamToGuid([])).toBe("");
    expect(routeParamToGuid("")).toBe("");
  });

  it("round-trips every GUID shape", () => {
    for (const guid of [
      "inst",
      "inst;db",
      "inst;db;;orders",
      "inst;db;public;order/items",
      "inst;db;public;50%_off",
      "inst;db;public;my table",
      "external:mysql:host:db:table",
    ]) {
      expect(routeParamToGuid(guidToRouteParams(guid))).toBe(guid);
    }
  });
});
