/**
 * The `allUsers` pseudo-member is server-managed.
 *
 * It matches every authenticated principal, including everyone who registers
 * later, so the server only accepts it bound to the `workspaceMember` baseline
 * (which every signed-in user holds implicitly) and rejects any policy that
 * dropped that binding. The IAM page therefore renders the binding read-only
 * and re-adds it to an editable draft, so a full replace always carries it.
 *
 * The server owns this rule; these helpers only keep the page from composing a
 * policy it would reject.
 */

export const ALL_USERS = "allUsers";
export const WORKSPACE_MEMBER_ROLE = "roles/workspaceMember";

export interface IamBindingDraft {
  role: string;
  members: string[];
}

/** Reports whether the binding is the server-managed allUsers baseline row. */
export function isAllUsersBaselineBinding(binding: IamBindingDraft): boolean {
  return (
    binding.role === WORKSPACE_MEMBER_ROLE &&
    binding.members.includes(ALL_USERS)
  );
}

/**
 * Returns the draft with the server-managed baseline binding present, adding
 * the row or the member when the stored policy does not carry it.
 */
export function withAllUsersBaselineBinding(
  bindings: IamBindingDraft[]
): IamBindingDraft[] {
  const index = bindings.findIndex(
    (binding) => binding.role === WORKSPACE_MEMBER_ROLE
  );
  if (index < 0) {
    return [...bindings, { role: WORKSPACE_MEMBER_ROLE, members: [ALL_USERS] }];
  }
  const binding = bindings[index];
  if (binding.members.includes(ALL_USERS)) {
    return bindings;
  }
  const next = [...bindings];
  next[index] = {
    role: binding.role,
    members: [ALL_USERS, ...binding.members],
  };
  return next;
}
