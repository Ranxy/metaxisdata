import { beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope, ref } from "vue";
import { checkPermissions } from "@/api/iam";
import { useResourcePermissions } from "./useResourcePermissions";

vi.mock("@/api/iam", () => ({ checkPermissions: vi.fn() }));

const mockedCheck = vi.mocked(checkPermissions);
const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.update",
  "metaxisdata.dataQuality.rules.run",
] as const;

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function setup(resourceId = "i;db;t") {
  const resource = ref<{ resourceType: string; resourceId: string } | null>({
    resourceType: "table",
    resourceId,
  });
  let result!: ReturnType<typeof useResourcePermissions>;
  const scope = effectScope();
  scope.run(() => {
    result = useResourcePermissions(resource, PERMISSIONS);
  });
  return { result, resource, scope };
}

describe("useResourcePermissions", () => {
  beforeEach(() => {
    mockedCheck.mockReset();
  });

  it("maps the server's answer onto the requested permissions", async () => {
    mockedCheck.mockResolvedValue([true, false]);
    const { result, scope } = setup();

    await flush();
    expect(mockedCheck).toHaveBeenCalledOnce();
    expect(mockedCheck).toHaveBeenCalledWith({
      resourceType: "table",
      resourceId: "i;db;t",
      permissions: PERMISSIONS,
    });
    expect(result.can(PERMISSIONS[0])).toBe(true);
    expect(result.can(PERMISSIONS[1])).toBe(false);
    expect(result.loading.value).toBe(false);
    scope.stop();
  });

  it("reloads when the resource changes", async () => {
    mockedCheck.mockResolvedValue([true, true]);
    const { result, resource, scope } = setup();

    await flush();
    expect(result.can(PERMISSIONS[1])).toBe(true);

    resource.value = { resourceType: "table", resourceId: "i;db;other" };
    await flush();
    expect(mockedCheck).toHaveBeenCalledTimes(2);
    expect(mockedCheck).toHaveBeenLastCalledWith({
      resourceType: "table",
      resourceId: "i;db;other",
      permissions: PERMISSIONS,
    });
    scope.stop();
  });

  it("hides every action when the check fails", async () => {
    mockedCheck.mockRejectedValue(new Error("offline"));
    const { result, scope } = setup();

    await flush();
    expect(result.can(PERMISSIONS[0])).toBe(false);
    expect(result.can(PERMISSIONS[1])).toBe(false);
    expect(result.loading.value).toBe(false);
    scope.stop();
  });

  it("asks nothing without a resource and keeps the actions hidden", async () => {
    const resource = ref<{ resourceType: string; resourceId: string } | null>(
      null
    );
    let result!: ReturnType<typeof useResourcePermissions>;
    const scope = effectScope();
    scope.run(() => {
      result = useResourcePermissions(resource, PERMISSIONS);
    });

    await flush();
    expect(mockedCheck).not.toHaveBeenCalled();
    expect(result.can(PERMISSIONS[0])).toBe(false);
    scope.stop();
  });

  it("keeps the newest answer when an older request resolves late", async () => {
    const first = deferred<boolean[]>();
    const second = deferred<boolean[]>();
    mockedCheck
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const { result, resource, scope } = setup();

    resource.value = { resourceType: "table", resourceId: "i;db;second" };
    await flush();
    expect(mockedCheck).toHaveBeenCalledTimes(2);

    second.resolve([false, true]);
    await flush();
    first.resolve([true, true]);
    await flush();

    expect(result.can(PERMISSIONS[0])).toBe(false);
    expect(result.can(PERMISSIONS[1])).toBe(true);
    scope.stop();
  });
});

it("clears the previous answer while a new resource is loading", async () => {
  const second = deferred<boolean[]>();
  mockedCheck
    .mockResolvedValueOnce([true, true])
    .mockReturnValueOnce(second.promise);
  const { result, resource, scope } = setup();

  await flush();
  expect(result.can(PERMISSIONS[0])).toBe(true);

  resource.value = { resourceType: "table", resourceId: "i;db;second" };
  await flush();
  expect(result.can(PERMISSIONS[0])).toBe(false);

  second.resolve([false, false]);
  await flush();
  expect(result.can(PERMISSIONS[0])).toBe(false);
  scope.stop();
});

it("ignores an in-flight answer after the resource disappears", async () => {
  const first = deferred<boolean[]>();
  mockedCheck.mockReturnValueOnce(first.promise);
  const { result, resource, scope } = setup();

  resource.value = null;
  await flush();
  first.resolve([true, true]);
  await flush();

  expect(result.can(PERMISSIONS[0])).toBe(false);
  expect(result.loading.value).toBe(false);
  scope.stop();
});
