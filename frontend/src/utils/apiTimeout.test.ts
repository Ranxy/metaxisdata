import { describe, expect, it, vi } from "vitest";
import {
  createRequestDeadline,
  DEFAULT_REQUEST_TIMEOUT_MS,
  requestTimeoutMs,
} from "./apiTimeout";

describe("requestTimeoutMs", () => {
  it("bounds an ordinary unary call", () => {
    expect(
      requestTimeoutMs("/metaxisdata.v1.DatabaseService/ListDatabases")
    ).toBe(DEFAULT_REQUEST_TIMEOUT_MS);
  });

  it("leaves provider and explanation calls alone", () => {
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

  it("leaves the LLM-backed data-quality work alone", () => {
    expect(
      requestTimeoutMs("/metaxisdata.v1.DataQualityService/SuggestQualityRule")
    ).toBeUndefined();
    expect(
      requestTimeoutMs("/metaxisdata.v1.DataQualityService/SuggestColumnLabels")
    ).toBeUndefined();
    expect(
      requestTimeoutMs("/metaxisdata.v1.DataQualityService/ProfileColumns")
    ).toBeUndefined();
  });

  it("leaves sync, run and push streams alone", () => {
    expect(
      requestTimeoutMs("/metaxisdata.v1.InstanceService/SyncInstance")
    ).toBeUndefined();
    expect(
      requestTimeoutMs("/metaxisdata.v1.InstanceService/BatchSyncInstances")
    ).toBeUndefined();
    expect(
      requestTimeoutMs("/metaxisdata.v1.DataQualityService/RunQualityRules")
    ).toBeUndefined();
    expect(
      requestTimeoutMs(
        "/metaxisdata.v1.NotificationService/SubscribeNotifications"
      )
    ).toBeUndefined();
  });

  it("leaves the streamed CSV and report exports alone", () => {
    // A large export walks or renders the same rows the list RPCs serve; the
    // default 60s ceiling would cancel it mid-file.
    for (const method of [
      "ExportQualityExecutions",
      "ExportQualityIssues",
      "ExportQualityTargets",
      "ExportQualityReport",
    ]) {
      expect(
        requestTimeoutMs(`/metaxisdata.v1.DataQualityService/${method}`)
      ).toBeUndefined();
    }
    expect(
      requestTimeoutMs("/metaxisdata.v1.AuditLogService/ListAuditLogs")
    ).toBe(DEFAULT_REQUEST_TIMEOUT_MS);
  });
});

describe("createRequestDeadline", () => {
  it("passes the caller's signal through when there is no deadline", () => {
    const controller = new AbortController();
    const deadline = createRequestDeadline(controller.signal, undefined);

    expect(deadline.signal).toBe(controller.signal);
    expect(deadline.timedOut()).toBe(false);
    deadline.cleanup();
  });

  it("has no signal when neither a caller nor a deadline provides one", () => {
    const deadline = createRequestDeadline(undefined, undefined);
    expect(deadline.signal).toBeUndefined();
    deadline.cleanup();
  });

  it("aborts and reports the deadline when it passes", async () => {
    vi.useFakeTimers();
    const deadline = createRequestDeadline(undefined, 50);

    expect(deadline.signal?.aborted).toBe(false);
    await vi.advanceTimersByTimeAsync(50);

    expect(deadline.signal?.aborted).toBe(true);
    expect(deadline.timedOut()).toBe(true);
    deadline.cleanup();
    vi.useRealTimers();
  });

  it("still honours the caller's own abort, which is not a timeout", () => {
    const controller = new AbortController();
    const deadline = createRequestDeadline(controller.signal, 60_000);

    controller.abort();

    expect(deadline.signal?.aborted).toBe(true);
    expect(deadline.timedOut()).toBe(false);
    deadline.cleanup();
  });

  it("does not abort after cleanup", async () => {
    vi.useFakeTimers();
    const deadline = createRequestDeadline(undefined, 50);

    deadline.cleanup();
    await vi.advanceTimersByTimeAsync(100);

    expect(deadline.signal?.aborted).toBe(false);
    expect(deadline.timedOut()).toBe(false);
    vi.useRealTimers();
  });

  it("forwards an already-aborted caller immediately", () => {
    const controller = new AbortController();
    controller.abort();

    const deadline = createRequestDeadline(controller.signal, 60_000);

    expect(deadline.signal?.aborted).toBe(true);
    expect(deadline.timedOut()).toBe(false);
    deadline.cleanup();
  });
});
