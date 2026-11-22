import { type Ref, ref, watch } from "vue";
import { checkPermissions } from "@/api/iam";
import type { Permission } from "@/lib/permissions";

/** One resource a page checks its actions against. */
export interface ResourceRef {
  resourceType: string;
  resourceId: string;
}

/**
 * The caller's permissions on one resource, resolved by the server's
 * self-service CheckPermissions RPC. `hasPermission` stays the workspace-level
 * answer; this composable is for a page that acts on a concrete target, where a
 * scoped grant can allow an action the workspace policy does not.
 *
 * It fails closed: a failed or incomplete check leaves every action hidden, and
 * the server still enforces the write.
 */
export function useResourcePermissions(
  resource: Ref<ResourceRef | null>,
  permissions: readonly Permission[]
) {
  const allowed = ref<ReadonlySet<string>>(new Set());
  const loading = ref(false);
  let request = 0;

  async function load() {
    const ref = resource.value;
    if (!ref || permissions.length === 0) {
      allowed.value = new Set();
      return;
    }
    const current = ++request;
    loading.value = true;
    try {
      const result = await checkPermissions({
        resourceType: ref.resourceType,
        resourceId: ref.resourceId,
        permissions,
      });
      if (current !== request) {
        return;
      }
      allowed.value = new Set(
        permissions.filter((_, index) => result[index] === true)
      );
    } catch {
      if (current !== request) {
        return;
      }
      allowed.value = new Set();
    } finally {
      if (current === request) {
        loading.value = false;
      }
    }
  }

  watch(resource, () => void load(), { immediate: true });

  function can(permission: Permission): boolean {
    return allowed.value.has(permission);
  }

  return { allowed, loading, can, reload: load };
}
