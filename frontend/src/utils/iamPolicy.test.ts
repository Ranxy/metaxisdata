import { describe, expect, it } from "vitest";
import {
  ALL_USERS,
  type IamBindingDraft,
  isAllUsersBaselineBinding,
  WORKSPACE_MEMBER_ROLE,
  withAllUsersBaselineBinding,
} from "./iamPolicy";

const baseline: IamBindingDraft = {
  role: WORKSPACE_MEMBER_ROLE,
  members: [ALL_USERS],
};

describe("isAllUsersBaselineBinding", () => {
  it("recognizes the server-managed row", () => {
    expect(isAllUsersBaselineBinding(baseline)).toBe(true);
    expect(
      isAllUsersBaselineBinding({
        role: WORKSPACE_MEMBER_ROLE,
        members: [ALL_USERS, "users/7"],
      })
    ).toBe(true);
  });

  it("rejects every other combination", () => {
    expect(
      isAllUsersBaselineBinding({ role: "roles/reader", members: [ALL_USERS] })
    ).toBe(false);
    expect(
      isAllUsersBaselineBinding({ role: WORKSPACE_MEMBER_ROLE, members: [] })
    ).toBe(false);
    expect(
      isAllUsersBaselineBinding({ role: "roles/reader", members: ["users/7"] })
    ).toBe(false);
  });
});

describe("withAllUsersBaselineBinding", () => {
  it("appends the row when the policy does not carry it", () => {
    const reader: IamBindingDraft = { role: "roles/reader", members: [] };
    const next = withAllUsersBaselineBinding([reader]);
    expect(next).toEqual([reader, baseline]);
  });

  it("leaves an existing row alone", () => {
    const bindings = [baseline];
    expect(withAllUsersBaselineBinding(bindings)).toBe(bindings);
  });

  it("adds the member to a baseline row that lost it", () => {
    const next = withAllUsersBaselineBinding([
      { role: WORKSPACE_MEMBER_ROLE, members: ["users/7"] },
    ]);
    expect(next).toEqual([
      { role: WORKSPACE_MEMBER_ROLE, members: [ALL_USERS, "users/7"] },
    ]);
  });
});
