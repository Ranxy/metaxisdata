import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { environmentId, environmentName } from "@/api/environment";
import { EnvironmentSchema } from "@/types/proto-es/v1/environment_service_pb";
import {
  ENVIRONMENT_COLOR_KEYS,
  environmentColorHex,
  environmentColorHexByKey,
  environmentColorKey,
} from "./environment";

function makeEnvironment(fields: { name: string; color?: string }) {
  return create(EnvironmentSchema, fields);
}

describe("environment resource names", () => {
  it("round-trips an id through the resource name", () => {
    expect(environmentName("prod")).toBe("environments/prod");
    expect(environmentId("environments/prod")).toBe("prod");
  });

  it("tolerates a bare id", () => {
    expect(environmentId("prod")).toBe("prod");
    expect(environmentId("")).toBe("");
  });
});

describe("environmentColorKey", () => {
  it("keeps a palette key the server assigned", () => {
    const environment = makeEnvironment({
      name: "environments/prod",
      color: "red",
    });
    expect(environmentColorKey(environment)).toBe("red");
    expect(environmentColorHex(environment)).toBe(
      environmentColorHexByKey("red")
    );
  });

  it("falls back to a stable palette entry when the color is empty", () => {
    // The two seeded environments have no color.
    const environment = makeEnvironment({ name: "environments/prod" });
    const key = environmentColorKey(environment);
    expect(ENVIRONMENT_COLOR_KEYS).toContain(key);
    // Stable across calls, so a row does not change color on re-render.
    expect(environmentColorKey(environment)).toBe(key);
  });

  it("ignores a color outside the palette", () => {
    const environment = makeEnvironment({
      name: "environments/prod",
      color: "chartreuse",
    });
    expect(ENVIRONMENT_COLOR_KEYS).toContain(environmentColorKey(environment));
  });
});
