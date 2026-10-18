import { describe, expect, it } from "vitest";
import { Engine } from "@/types/proto-es/v1/common_pb";
import {
  engineBadgeClass,
  engineBgClass,
  engineIcon,
  engineLabel,
  engineTextClass,
} from "./engine";

const ENGINES = [
  Engine.MYSQL,
  Engine.POSTGRES,
  Engine.TIDB,
  Engine.MARIADB,
  Engine.OCEANBASE,
  Engine.STARROCKS,
  Engine.DORIS,
  Engine.MSSQL,
];

describe("engine presentation", () => {
  it("labels every supported engine instead of falling back", () => {
    for (const engine of ENGINES) {
      expect(engineLabel(engine)).not.toBe("Unknown");
      expect(engineIcon(engine)).not.toBe("DB");
    }
  });

  it("colors the badge pill and the avatar consistently", () => {
    expect(engineBadgeClass(Engine.MYSQL)).toContain("bg-warning-subtle");
    expect(engineBgClass(Engine.MYSQL)).toBe("bg-warning-subtle");
    expect(engineTextClass(Engine.MYSQL)).toBe("text-warning");
  });

  it("falls back for an unspecified or missing engine", () => {
    expect(engineLabel(Engine.ENGINE_UNSPECIFIED)).toBe("Unknown");
    expect(engineIcon(Engine.ENGINE_UNSPECIFIED)).toBe("DB");
    expect(engineBadgeClass(Engine.ENGINE_UNSPECIFIED)).toContain("bg-muted");
    expect(engineLabel(undefined)).toBe("Unknown");
    expect(engineBgClass(undefined)).toBe("bg-muted");
  });
});
