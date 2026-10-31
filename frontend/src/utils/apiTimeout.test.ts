import { describe, expect, it } from "vitest";
import {
  DEFAULT_REQUEST_TIMEOUT_MS,
  requestTimeoutMs,
  withRequestTimeout,
} from "./apiTimeout";

describe("requestTimeoutMs", () => {
  it("bounds an ordinary unary call", () => {
    expect(
      requestTimeoutMs("/metaxisdata.v1.DatabaseService/ListDatabases")
    ).toBe(DEFAULT_REQUEST_TIMEOUT_MS);
  });

  it("leaves LLM and explanation streams alone", () => {
    expect(
      requestTimeoutMs("/metaxisdata.v1.LLMService/ListProfiles")
    ).toBeUndefined();
    expect(
      requestTimeoutMs("/metaxisdata.v1.ExplainSQLService/ExplainSQL")
    ).toBeUndefined();
    expect(
      requestTimeoutMs(
        "/metaxisdata.v1.DataQualityService/ExplainQualityExecution"
      )
    ).toBeUndefined();
  });

  it("leaves sync and run paths alone", () => {
    expect(
      requestTimeoutMs("/metaxisdata.v1.InstanceService/SyncInstance")
    ).toBeUndefined();
    expect(
      requestTimeoutMs("/metaxisdata.v1.DataQualityService/RunQualityRules")
    ).toBeUndefined();
  });
});

describe("withRequestTimeout", () => {
  it("keeps the caller's signal when there is no deadline", () => {
    const controller = new AbortController();
    expect(withRequestTimeout(controller.signal, undefined)).toBe(
      controller.signal
    );
    expect(withRequestTimeout(undefined, undefined)).toBeUndefined();
  });

  it("aborts when the deadline passes", async () => {
    const signal = withRequestTimeout(undefined, 5);
    expect(signal?.aborted).toBe(false);
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(signal?.aborted).toBe(true);
  });

  it("still honours the caller's own abort", () => {
    const controller = new AbortController();
    const signal = withRequestTimeout(controller.signal, 60_000);

    controller.abort();

    expect(signal?.aborted).toBe(true);
  });
});
