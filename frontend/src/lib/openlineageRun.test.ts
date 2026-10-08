import { describe, expect, it } from "vitest";
import {
  openlineageJobName,
  openlineageRunLabel,
  parseOpenLineageRun,
} from "./openlineageRun";

const runGUID =
  "openlineage:run:TASK:default:e2e_02_pg_transform.dwd_load:01a0e120-e4ed-70fd-8ec1-f223d35622d4";

describe("parseOpenLineageRun", () => {
  it("splits the scope off the job and the run id", () => {
    expect(parseOpenLineageRun(runGUID)).toEqual({
      jobName: "e2e_02_pg_transform.dwd_load",
      runID: "01a0e120-e4ed-70fd-8ec1-f223d35622d4",
    });
  });

  it("decodes a percent-encoded job name", () => {
    expect(
      parseOpenLineageRun("openlineage:run:TASK:a%20b:job%2Fname:run-id")
    ).toEqual({ jobName: "job/name", runID: "run-id" });
  });

  it("refuses anything that is not a run", () => {
    expect(parseOpenLineageRun("test-pg-1;e2e;public;orders")).toBeNull();
    expect(parseOpenLineageRun("openlineage:run:")).toBeNull();
    expect(parseOpenLineageRun("openlineage:run:job")).toBeNull();
  });
});

describe("openlineageRunLabel", () => {
  it("names the job and the run id", () => {
    expect(openlineageRunLabel(runGUID)).toBe(
      "e2e_02_pg_transform.dwd_load · 01a0e120-e4ed-70fd-8ec1-f223d35622d4"
    );
  });

  it("returns a suffix too short to name a run unchanged", () => {
    // A run GUID carries `<job type>:<namespace>:<job>:<run id>`; anything shorter
    // is not one, and guessing a job out of it would invent a run.
    expect(openlineageRunLabel("openlineage:run:job:run")).toBe(
      "openlineage:run:job:run"
    );
  });

  it("returns a GUID that is not a run unchanged", () => {
    expect(openlineageRunLabel("test-pg-1;e2e;public;orders")).toBe(
      "test-pg-1;e2e;public;orders"
    );
    expect(openlineageRunLabel("openlineage:run:")).toBe("openlineage:run:");
  });
});

describe("openlineageJobName", () => {
  it("drops the run id", () => {
    expect(openlineageJobName(runGUID)).toBe("e2e_02_pg_transform.dwd_load");
  });

  it("returns a GUID that is not a run unchanged", () => {
    expect(openlineageJobName("external:default:orders")).toBe(
      "external:default:orders"
    );
  });
});
