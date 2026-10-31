import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { useDataQualityPermissions } from "./useDataQualityPermissions";

function grant(permissions: string[]) {
  const store = useAuthStore();
  store.user = create(UserSchema, { name: "users/me", permissions });
  return useDataQualityPermissions();
}

describe("useDataQualityPermissions", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
  });

  it("grants each check from its own catalog identifier", () => {
    const can = grant([
      "metaxisdata.dataQuality.rules.get",
      "metaxisdata.dataQuality.rules.run",
      "metaxisdata.dataQuality.rules.create",
      "metaxisdata.dataQuality.rules.update",
      "metaxisdata.dataQuality.rules.delete",
      "metaxisdata.dataQuality.suggest",
      "metaxisdata.dataQuality.profile.run",
      "metaxisdata.dataQuality.issues.list",
      "metaxisdata.dataQuality.issues.get",
      "metaxisdata.dataQuality.issues.update",
      "metaxisdata.dataQuality.contracts.publish",
      "metaxisdata.dataQuality.executions.get",
      "metaxisdata.dataQuality.executions.list",
      "metaxisdata.dataQuality.explain",
    ]);

    expect(can.canViewRule.value).toBe(true);
    expect(can.canRun.value).toBe(true);
    expect(can.canCreate.value).toBe(true);
    expect(can.canUpdate.value).toBe(true);
    expect(can.canDelete.value).toBe(true);
    expect(can.canSuggest.value).toBe(true);
    expect(can.canProfile.value).toBe(true);
    expect(can.canListIssues.value).toBe(true);
    expect(can.canViewIssue.value).toBe(true);
    expect(can.canUpdateIssue.value).toBe(true);
    expect(can.canPublish.value).toBe(true);
    expect(can.canViewExecution.value).toBe(true);
    expect(can.canListExecutions.value).toBe(true);
    expect(can.canExplain.value).toBe(true);
  });

  it("denies everything the caller does not hold", () => {
    const can = grant(["metaxisdata.dataQuality.rules.list"]);

    expect(can.canRun.value).toBe(false);
    expect(can.canCreate.value).toBe(false);
    expect(can.canPublish.value).toBe(false);
    expect(can.canExplain.value).toBe(false);
  });

  it("maps every property to exactly one catalog identifier", () => {
    const matrix: Array<
      [keyof ReturnType<typeof useDataQualityPermissions>, string]
    > = [
      ["canViewRule", "metaxisdata.dataQuality.rules.get"],
      ["canRun", "metaxisdata.dataQuality.rules.run"],
      ["canCreate", "metaxisdata.dataQuality.rules.create"],
      ["canUpdate", "metaxisdata.dataQuality.rules.update"],
      ["canDelete", "metaxisdata.dataQuality.rules.delete"],
      ["canSuggest", "metaxisdata.dataQuality.suggest"],
      ["canProfile", "metaxisdata.dataQuality.profile.run"],
      ["canListIssues", "metaxisdata.dataQuality.issues.list"],
      ["canViewIssue", "metaxisdata.dataQuality.issues.get"],
      ["canUpdateIssue", "metaxisdata.dataQuality.issues.update"],
      ["canPublish", "metaxisdata.dataQuality.contracts.publish"],
      ["canViewExecution", "metaxisdata.dataQuality.executions.get"],
      ["canListExecutions", "metaxisdata.dataQuality.executions.list"],
      ["canExplain", "metaxisdata.dataQuality.explain"],
    ];

    for (const [property, identifier] of matrix) {
      const can = grant([identifier]);
      for (const [other] of matrix) {
        expect(can[other].value, `${other} under ${identifier}`).toBe(
          other === property
        );
      }
    }
  });

  it("recomputes when the permission set changes", () => {
    const can = grant([]);
    expect(can.canRun.value).toBe(false);

    const store = useAuthStore();
    store.user = create(UserSchema, {
      name: "users/me",
      permissions: ["metaxisdata.dataQuality.rules.run"],
    });

    expect(can.canRun.value).toBe(true);
  });

  it("denies when there is no profile loaded yet", () => {
    const can = useDataQualityPermissions();

    expect(can.canRun.value).toBe(false);
  });
});
