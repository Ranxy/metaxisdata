<template>
  <div class="space-y-4">
    <PageHeader :title="t('applicationSql.title')" />

    <Card>
      <!-- One row of fields. Every field is the same shape — a label over a 40px control —
           so the labels share one baseline and the controls, including the button, share
           another. A radix Select renders a hidden 1px native <select> inside its wrapper,
           which is what used to push its label 4px above the others. -->
      <div class="flex flex-wrap items-end gap-3 border-b p-4">
        <div class="flex flex-col gap-1.5 [&>select]:hidden">
          <Label
            for="application-sql-instance"
            class="text-sm font-medium leading-none"
          >
            {{ t("applicationSql.instance") }}
          </Label>
          <Select v-model="instanceID">
            <SelectTrigger
              id="application-sql-instance"
              class="w-44"
            >
              <SelectValue :placeholder="t('applicationSql.allInstances')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-">
                {{ t("applicationSql.allInstances") }}
              </SelectItem>
              <SelectItem
                v-for="instance in instanceStore.active"
                :key="instance.name"
                :value="instance.name.split('/').pop() ?? ''"
              >
                {{ instance.title || instance.name }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <ApplicationSelect
          v-model="applicationID"
          :label="t('applicationSql.application')"
          :placeholder="t('applicationSql.anyApplication')"
          class="w-56"
        />

        <div class="flex flex-col gap-1.5 [&>select]:hidden">
          <Label
            for="application-sql-kind"
            class="text-sm font-medium leading-none"
          >
            {{ t("applicationSql.kind") }}
          </Label>
          <Select v-model="kind">
            <SelectTrigger
              id="application-sql-kind"
              class="w-32"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <!-- `-` is the wildcard the instance filter also uses: radix refuses an
                   empty value, and the service reads `-` as "any". -->
              <SelectItem value="-">
                {{ t("applicationSql.anyKind") }}
              </SelectItem>
              <SelectItem value="read">
                {{ t("applicationSql.read") }}
              </SelectItem>
              <SelectItem value="write">
                {{ t("applicationSql.write") }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div class="flex flex-col gap-1.5">
          <Label
            for="application-sql-query"
            class="text-sm font-medium leading-none"
          >
            {{ t("applicationSql.search") }}
          </Label>
          <Input
            id="application-sql-query"
            v-model="query"
            class="w-64"
            :placeholder="t('applicationSql.searchPlaceholder')"
          />
        </div>

        <Button
          variant="outline"
          @click="applyFilters"
        >
          {{ t("applicationSql.apply") }}
        </Button>
      </div>

      <PageState
        :loading="isLoading"
        :error="error"
      >
        <EmptyState
          v-if="statements.length === 0"
          :icon="FileCode2"
          :title="t('applicationSql.noStatements')"
          :description="t('applicationSql.noStatementsHint')"
        />

        <div v-else>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{{ t("applicationSql.statement") }}</TableHead>
                <TableHead class="w-24">{{ t("applicationSql.kind") }}</TableHead>
                <TableHead>{{ t("applicationSql.scope") }}</TableHead>
                <TableHead class="w-28 text-right">
                  {{ t("applicationSql.executions") }}
                </TableHead>
                <TableHead class="w-44">{{ t("applicationSql.lastSeen") }}</TableHead>
                <TableHead class="w-24 text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow
                v-for="statement in statements"
                :key="statement.guid"
              >
                <TableCell>
                  <button
                    type="button"
                    class="max-w-xl truncate text-left font-mono text-xs hover:underline"
                    :title="statement.sqlText"
                    @click="openDetail(statement.guid)"
                  >
                    {{ firstLine(statement.sqlText) }}
                  </button>
                </TableCell>
                <TableCell>
                  <Badge :variant="statement.kind === ApplicationSQLKind.APPLICATION_SQL_KIND_WRITE ? 'default' : 'secondary'">
                    {{ kindLabel(statement.kind) }}
                  </Badge>
                </TableCell>
                <TableCell class="text-xs text-muted-foreground">
                  {{ scopeLabel(statement) }}
                </TableCell>
                <TableCell class="text-right text-sm tabular-nums">
                  {{ statement.executionCount }}
                </TableCell>
                <TableCell class="text-sm">
                  {{ formatDateTime(statement.lastSeenAt, locale) }}
                </TableCell>
                <TableCell class="text-right">
                  <Button
                    v-if="canDelete"
                    variant="outline"
                    size="sm"
                    @click="confirmDelete(statement.guid)"
                  >
                    <Trash2 class="h-4 w-4" />
                  </Button>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

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
    </Card>

    <!--
      The statement is read-only. It is a machine fact, not an asset: the parameters are
      whatever the application actually ran, and editing the text would detach it from the
      fingerprint that identifies it.
    -->
    <Dialog
      :open="detailOpen"
      @update:open="(open: boolean) => (detailOpen = open)"
    >
      <DialogContent class="max-w-3xl">
        <DialogHeader>
          <DialogTitle>{{ t("applicationSql.detail") }}</DialogTitle>
          <DialogDescription>
            {{ t("applicationSql.detailHint") }}
          </DialogDescription>
        </DialogHeader>

        <div
          v-if="detail"
          class="space-y-3"
        >
          <pre
            class="max-h-72 overflow-auto rounded-md border bg-muted p-3 text-xs"
            data-testid="statement-text"
          >{{ detail.sqlText }}</pre>

          <div class="grid grid-cols-2 gap-2 text-sm">
            <div>
              <div class="text-xs text-muted-foreground">
                {{ t("applicationSql.scope") }}
              </div>
              <div>{{ scopeLabel(detail) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">
                {{ t("applicationSql.kind") }}
              </div>
              <div>{{ kindLabel(detail.kind) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">
                {{ t("applicationSql.firstSeen") }}
              </div>
              <div>{{ formatDateTime(detail.firstSeenAt, locale) }}</div>
            </div>
            <div>
              <div class="text-xs text-muted-foreground">
                {{ t("applicationSql.lastSeen") }}
              </div>
              <div>{{ formatDateTime(detail.lastSeenAt, locale) }}</div>
            </div>
          </div>

          <div>
            <div class="mb-1 text-xs text-muted-foreground">
              {{ t("applicationSql.reportedBy") }}
            </div>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{{ t("applicationSql.application") }}</TableHead>
                  <TableHead class="w-28 text-right">
                    {{ t("applicationSql.executions") }}
                  </TableHead>
                  <TableHead class="w-44">{{ t("applicationSql.lastSeen") }}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="observation in detail.observations"
                  :key="observation.application"
                >
                  <TableCell>
                    {{ observation.applicationDisplayName || observation.application }}
                  </TableCell>
                  <TableCell class="text-right text-sm tabular-nums">
                    {{ observation.executionCount }}
                  </TableCell>
                  <TableCell class="text-sm">
                    {{ formatDateTime(observation.lastSeenAt, locale) }}
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>

          <div class="font-mono text-xs text-muted-foreground">
            {{ detail.fingerprint }}
          </div>
        </div>

        <DialogFooter>
          <Button @click="detailOpen = false">{{ t("common.close") }}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <ConfirmDeleteDialog
      v-model="deleteOpen"
      :title="t('applicationSql.deleteTitle')"
      :message="t('applicationSql.deleteConfirm')"
      :loading="saving"
      @confirm="handleDelete"
    />
  </div>
</template>

<script setup lang="ts">
import { FileCode2, Trash2 } from "lucide-vue-next";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import {
  deleteApplicationSQL,
  getApplicationSQL,
  listApplicationSQLs,
} from "@/api/application";
import ApplicationSelect from "@/components/application/ApplicationSelect.vue";
import ConfirmDeleteDialog from "@/components/common/ConfirmDeleteDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import TablePager from "@/components/common/TablePager.vue";
import PageHeader from "@/components/layout/PageHeader.vue";
import Badge from "@/components/ui/badge/Badge.vue";
import Button from "@/components/ui/button/Button.vue";
import Card from "@/components/ui/card/Card.vue";
import Dialog from "@/components/ui/dialog/Dialog.vue";
import DialogContent from "@/components/ui/dialog/DialogContent.vue";
import DialogDescription from "@/components/ui/dialog/DialogDescription.vue";
import DialogFooter from "@/components/ui/dialog/DialogFooter.vue";
import DialogHeader from "@/components/ui/dialog/DialogHeader.vue";
import DialogTitle from "@/components/ui/dialog/DialogTitle.vue";
import Input from "@/components/ui/input/Input.vue";
import Label from "@/components/ui/label/Label.vue";
import Select from "@/components/ui/select/Select.vue";
import SelectContent from "@/components/ui/select/SelectContent.vue";
import SelectItem from "@/components/ui/select/SelectItem.vue";
import SelectTrigger from "@/components/ui/select/SelectTrigger.vue";
import SelectValue from "@/components/ui/select/SelectValue.vue";
import Table from "@/components/ui/table/Table.vue";
import TableBody from "@/components/ui/table/TableBody.vue";
import TableCell from "@/components/ui/table/TableCell.vue";
import TableHead from "@/components/ui/table/TableHead.vue";
import TableHeader from "@/components/ui/table/TableHeader.vue";
import TableRow from "@/components/ui/table/TableRow.vue";
import { useErrorHandler } from "@/composables/useErrorHandler";
import { usePagedFetch } from "@/composables/usePagedFetch";
import { notify } from "@/lib/notify";
import { useAuthStore } from "@/store/modules/auth";
import { useInstanceStore } from "@/store/modules/instance";
import {
  type ApplicationSQL,
  ApplicationSQLKind,
} from "@/types/proto-es/v1/application_service_pb";
import { formatDateTime } from "@/utils/datetime";

const { t, locale } = useI18n();
const { handleError } = useErrorHandler();
const authStore = useAuthStore();
const instanceStore = useInstanceStore();

const canDelete = computed(() =>
  authStore.hasPermission("metaxisdata.applicationSqls.delete")
);

const route = useRoute();
const error = ref("");
const saving = ref(false);
const pageSize = ref(50);

// The applied filters, distinct from the inputs so that typing does not re-query.
const instanceID = ref("-");
const appliedInstanceID = ref("-");
const applicationID = ref("");
const appliedApplicationID = ref("");
// The select's own values are strings; the enum is built at the request boundary, where it
// belongs. `-` is the "any kind" item and the value the filter starts on.
const kind = ref("-");
const appliedKind = ref("-");
const query = ref("");
const appliedQuery = ref("");

const detailOpen = ref(false);
const detail = ref<ApplicationSQL>();
const deleteOpen = ref(false);
const deletingGUID = ref("");

const {
  items: statements,
  isLoading,
  hasNext,
  hasPrevious,
  reset: resetPage,
  refresh: refreshPage,
  goNext: goToNextPage,
  goPrevious: goToPreviousPage,
} = usePagedFetch<ApplicationSQL>({
  fetchPage: async (pageToken, signal) => {
    // `-` is a wildcard segment: the service reads it as "any instance" / "any database",
    // which is what a viewer who has not picked a scope wants to see.
    const response = await listApplicationSQLs({
      parent: `instances/${appliedInstanceID.value}/databases/-`,
      pageSize: pageSize.value,
      pageToken,
      application: appliedApplicationID.value,
      kind: kindFromFilter(appliedKind.value),
      query: appliedQuery.value,
      signal,
    });
    return {
      items: response.applicationSqls,
      nextPageToken: response.nextPageToken,
    };
  },
  onError: (e) => {
    error.value = handleError(e, "applicationSql.fetchError");
  },
});

watch(pageSize, () => void resetPage());

function applyFilters() {
  appliedInstanceID.value = instanceID.value;
  appliedApplicationID.value = applicationID.value.trim();
  appliedKind.value = kind.value;
  appliedQuery.value = query.value.trim();
  void resetPage();
}

// `-`, and anything the select does not know, means the filter is off.
function kindFromFilter(value: string): ApplicationSQLKind {
  switch (value) {
    case "read":
      return ApplicationSQLKind.APPLICATION_SQL_KIND_READ;
    case "write":
      return ApplicationSQLKind.APPLICATION_SQL_KIND_WRITE;
    default:
      return ApplicationSQLKind.APPLICATION_SQL_KIND_UNSPECIFIED;
  }
}

function firstLine(text: string): string {
  return text.split("\n")[0] ?? "";
}

function kindLabel(value: ApplicationSQLKind): string {
  switch (value) {
    case ApplicationSQLKind.APPLICATION_SQL_KIND_WRITE:
      return t("applicationSql.write");
    case ApplicationSQLKind.APPLICATION_SQL_KIND_READ:
      return t("applicationSql.read");
    case ApplicationSQLKind.APPLICATION_SQL_KIND_UNKNOWN:
      return t("applicationSql.unknownKind");
    default:
      return "-";
  }
}

function scopeLabel(statement: ApplicationSQL): string {
  const parts = [statement.instanceResourceId, statement.databaseName];
  if (statement.schemaName) parts.push(statement.schemaName);
  return parts.filter(Boolean).join(" · ");
}

async function openDetail(guid: string) {
  try {
    detail.value = await getApplicationSQL(guid);
    detailOpen.value = true;
  } catch (e: unknown) {
    handleError(e, "applicationSql.detailFetchError");
  }
}

function confirmDelete(guid: string) {
  deletingGUID.value = guid;
  deleteOpen.value = true;
}

async function handleDelete() {
  saving.value = true;
  try {
    await deleteApplicationSQL(deletingGUID.value);
    notify.success(t("applicationSql.deleted"));
    deleteOpen.value = false;
    await refreshPage();
  } catch (e: unknown) {
    handleError(e, "applicationSql.deleteError");
  } finally {
    saving.value = false;
  }
}

onMounted(async () => {
  await Promise.all([instanceStore.ensureLoaded(), resetPage()]);

  // A link can name one statement, which is how the lineage canvas opens the row it drew: the
  // dialog is fetched by GUID, so it does not need the statement to be on the page it landed on.
  const guid = typeof route.query.guid === "string" ? route.query.guid : "";
  if (guid) {
    await openDetail(guid);
  }
});
</script>
