<template>
  <div class="space-y-4">
    <!-- The filter bar is administrative chrome: a self-service reader has a
         handful of rows and nothing to narrow. -->
    <div
      v-if="scope === 'admin'"
      class="flex items-start gap-3"
    >
      <AdvancedSearchBar
        class="min-w-0 flex-1"
        :filter-categories="filterCategories"
        :search-placeholder="t('connectedApps.searchPlaceholder')"
        @update:filters="handleFiltersUpdate"
      />
    </div>

    <Card>
      <CardContent class="space-y-4 pt-6">
        <PageState
          :loading="isLoading"
          :error="error"
        >
          <EmptyState
            v-if="apps.length === 0"
            :icon="Link2"
            :title="t('connectedApps.emptyTitle')"
            :description="
              scope === 'admin'
                ? t('connectedApps.emptyAdminDescription')
                : t('connectedApps.emptySelfDescription')
            "
          />

          <div v-else class="space-y-4">
            <ConnectedAppTable
              :apps="apps"
              :show-user="scope === 'admin'"
              @revoke="confirmRevoke"
            />

            <TablePager
              v-model:page-size="pageSize"
              :has-previous="hasPrevious"
              :has-next="hasNext"
              :disabled="isLoading"
              @previous="goToPreviousPage"
              @next="goToNextPage"
            />
          </div>
        </PageState>
      </CardContent>
    </Card>

    <!-- Revoking is not deleting: the record survives as a tombstone, which is
         why the dialog is handed the operation's own verb. -->
    <ConfirmDeleteDialog
      v-model="revokeOpen"
      :title="t('connectedApps.revokeTitle')"
      :message="t('connectedApps.revokeConfirm')"
      :item-name="revoking?.clientName || revoking?.clientId"
      :loading="isRevoking"
      :confirm-label="t('connectedApps.revoke')"
      @confirm="handleRevoke"
    />
  </div>
</template>

<script setup lang="ts">
import { Link2 } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listConnectedApps, revokeConnectedApp } from "@/api/connected-app";
import { listAllUsers } from "@/api/user";
import AdvancedSearchBar, {
  type ActiveFilter,
  type FilterCategory,
  type FilterOption,
} from "@/components/common/AdvancedSearchBar.vue";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import ConnectedAppTable from "@/components/connected-app/ConnectedAppTable.vue";
import { Card, CardContent } from "@/components/ui/card";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { useAuthStore } from "@/store/modules/auth";
import type { ConnectedApp } from "@/types/proto-es/v1/connected_app_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

const props = defineProps<{
  /** Administrators list the workspace and may filter; self-service always
   *  pins the query to the signed-in user. */
  scope: "admin" | "self";
}>();

const { t } = useI18n();
const authStore = useAuthStore();
const { handleError, showSuccess } = useErrorHandler();

const pageSize = ref(50);
const error = ref("");
const revokeOpen = ref(false);
const revoking = ref<ConnectedApp>();
const isRevoking = ref(false);

// Admin filter state, owned by the search bar and read back from its pills.
const searchText = ref("");
const userFilter = ref("");
const stateFilter = ref("");
const clientFilter = ref("");

const stateOptions = computed<FilterOption[]>(() => [
  { value: "ACTIVE", label: t("connectedApps.statusActive") },
  { value: "REVOKED", label: t("connectedApps.statusRevoked") },
  { value: "EXPIRED", label: t("connectedApps.statusExpired") },
  {
    value: "REAUTH_REQUIRED",
    label: t("connectedApps.statusReauthRequired"),
  },
  { value: "INACTIVE", label: t("connectedApps.statusInactive") },
]);

/**
 * The user picker reads the workspace's accounts, which needs users.list. A
 * connected-apps administrator without that permission simply loses the picker:
 * the email/client search still narrows the list, and the server would refuse to
 * disclose the accounts anyway.
 */
let userOptionsPromise: Promise<FilterOption[]> | null = null;
function loadUserOptions(): Promise<FilterOption[]> {
  if (!authStore.hasPermission("metaxisdata.users.list")) {
    return Promise.resolve([]);
  }
  userOptionsPromise ??= listAllUsers()
    .then((users) =>
      users
        .filter((user) => !!user.name)
        .map((user) => ({ value: user.name, label: userLabel(user) }))
    )
    .catch(() => {
      userOptionsPromise = null;
      return [];
    });
  return userOptionsPromise;
}

function userLabel(user: User): string {
  if (user.title && user.email) {
    return `${user.title} (${user.email})`;
  }
  return user.title || user.email || user.name;
}

/** The filter dimensions the bar offers; null while the caller cannot read
 *  users, because an empty picker is worse than no picker. */
const filterCategories = computed<FilterCategory[]>(() => {
  const categories: FilterCategory[] = [];
  if (authStore.hasPermission("metaxisdata.users.list")) {
    categories.push({
      type: "user",
      label: t("connectedApps.filterUser"),
      icon: "👤",
      options: loadUserOptions,
    });
  }
  categories.push(
    {
      type: "state",
      label: t("connectedApps.filterState"),
      icon: "🚦",
      options: stateOptions.value,
    },
    {
      type: "client",
      label: t("connectedApps.filterClient"),
      icon: "🖥️",
      kind: "input",
    }
  );
  return categories;
});

function handleFiltersUpdate(filters: ActiveFilter[]) {
  const valueOf = (type: string) =>
    filters.find((filter) => filter.type === type)?.value ?? "";
  searchText.value = valueOf("name");
  userFilter.value = valueOf("user");
  stateFilter.value = valueOf("state");
  clientFilter.value = valueOf("client");
}

// The filter only narrows the rows the caller may already see; the server owns
// the state comparison so the page never has to guess a row's state from dates.
const filterExpression = computed(() => {
  const clauses: string[] = [];
  if (userFilter.value) {
    clauses.push(`user == ${JSON.stringify(userFilter.value)}`);
  }
  if (stateFilter.value) {
    clauses.push(`state == ${JSON.stringify(stateFilter.value)}`);
  }
  const client = clientFilter.value.trim();
  if (client) {
    clauses.push(`client.matches(${JSON.stringify(client)})`);
  }
  // The search box is the broad text match: a client name, an opaque client id
  // or an email, whichever the caller happens to have.
  const query = searchText.value.trim();
  if (query) {
    const quoted = JSON.stringify(query);
    clauses.push(
      `(client.matches(${quoted}) || client_id.matches(${quoted}) || user_email.matches(${quoted}))`
    );
  }
  return clauses.join(" && ");
});

const {
  items: apps,
  isLoading,
  hasNext,
  hasPrevious,
  reset,
  refresh,
  goNext: goToNextPage,
  goPrevious: goToPreviousPage,
} = usePagedFetch<ConnectedApp>({
  fetchPage: async (pageToken, signal) => {
    error.value = "";
    const response = await listConnectedApps({
      pageSize: pageSize.value,
      pageToken,
      // The self-service page pins itself to the caller even when an
      // administrator opens it, so "my connected apps" never means the
      // workspace's.
      user: props.scope === "self" ? (authStore.user?.name ?? "") : "",
      filter: props.scope === "admin" ? filterExpression.value : "",
      signal,
    });
    return {
      items: response.connectedApps,
      nextPageToken: response.nextPageToken,
    };
  },
  onError: (err) => {
    error.value = handleError(err, "connectedApps.fetchError");
  },
});

// A cursor belongs to the query that produced it, so a changed page size or any
// filter restarts the walk at the first page.
watch(pageSize, () => void reset());
watch([userFilter, stateFilter, clientFilter], () => void reset());

// The bar emits on every keystroke, so the search box restarts the walk on a
// short debounce rather than firing one request per character.
let searchTimer: ReturnType<typeof setTimeout> | null = null;
watch(searchText, () => {
  if (searchTimer) {
    clearTimeout(searchTimer);
  }
  searchTimer = setTimeout(() => {
    searchTimer = null;
    void reset();
  }, 250);
});

onBeforeUnmount(() => {
  if (searchTimer) {
    clearTimeout(searchTimer);
  }
});

function confirmRevoke(app: ConnectedApp) {
  revoking.value = app;
  revokeOpen.value = true;
}

async function handleRevoke() {
  if (!revoking.value) {
    return;
  }
  isRevoking.value = true;
  try {
    await revokeConnectedApp(revoking.value.name);
    revokeOpen.value = false;
    showSuccess("connectedApps.revoked");
    // Revoking the only row on a page would leave the pager on an empty page;
    // step back instead, and otherwise reload the page the reader is on.
    if (apps.value.length === 1 && hasPrevious.value) {
      await goToPreviousPage();
    } else {
      await refresh();
    }
  } catch (err) {
    handleError(err, "connectedApps.revokeError");
  } finally {
    isRevoking.value = false;
  }
}

onMounted(() => {
  void reset();
});

defineExpose({ refresh, isLoading });
</script>
