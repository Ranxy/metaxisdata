import { computed, ref } from "vue";
import { listAllUsers } from "@/api/user";
import { useAuthStore } from "@/store/modules/auth";
import type { User } from "@/types/proto-es/v1/user_service_pb";

/**
 * The workspace member directory, for naming the people a rule points at.
 *
 * A page loads it once (the list page also hands `users` to the owner picker,
 * so the picker issues no request of its own) and resolves owners with
 * `emailOf`. Names the directory cannot resolve — an account deleted since the
 * rule was written, or a directory request that failed — keep the raw
 * `users/{id}` resource name rather than an empty cell.
 */
export function useUserDirectory() {
  const authStore = useAuthStore();
  const users = ref<User[]>([]);
  let loaded = false;

  const emailsByName = computed(
    () => new Map(users.value.map((user) => [user.name, user.email]))
  );

  function emailOf(name: string): string {
    return emailsByName.value.get(name) || name;
  }

  /**
   * Load the directory, once the caller may read it and only the first time:
   * the pages that share it may ask on every navigation. A failure is silent;
   * owners keep their resource names.
   */
  async function load() {
    if (loaded || !authStore.hasPermission("metaxisdata.users.list")) {
      return;
    }
    loaded = true;
    try {
      users.value = await listAllUsers();
    } catch {
      users.value = [];
    }
  }

  return { users, emailOf, load };
}
