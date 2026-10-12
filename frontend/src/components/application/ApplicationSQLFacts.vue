<template>
  <div class="space-y-3">
    <!-- Every field is a bordered chip, the shape every other metadata detail page
         uses for a flat set of facts. The figures used to sit in a borderless grid,
         which left the fingerprint as an unlabelled hash in the middle of the page. -->
    <div class="flex flex-wrap gap-2">
      <div class="rounded-md border px-3 py-2">
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.kind") }}
        </div>
        <Badge
          :variant="
            statement.kind === ApplicationSQLKind.APPLICATION_SQL_KIND_WRITE
              ? 'default'
              : 'secondary'
          "
        >
          {{ kindLabel }}
        </Badge>
      </div>
      <div class="rounded-md border px-3 py-2">
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.executions") }}
        </div>
        <div class="text-sm font-medium tabular-nums">
          {{ statement.executionCount }}
        </div>
      </div>
      <div class="rounded-md border px-3 py-2">
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.firstSeen") }}
        </div>
        <div class="text-sm font-medium">
          {{ formatDateTime(statement.firstSeenAt, locale) }}
        </div>
      </div>
      <div class="rounded-md border px-3 py-2">
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.lastSeen") }}
        </div>
        <div class="text-sm font-medium">
          {{ formatDateTime(statement.lastSeenAt, locale) }}
        </div>
      </div>
      <!-- The fingerprint is the statement's identity across every application that
           reports it, and the one value on this page a reader copies elsewhere. -->
      <div class="min-w-[22rem] flex-1 rounded-md border px-3 py-2">
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.fingerprint") }}
        </div>
        <div class="font-mono text-xs break-all">
          {{ statement.fingerprint }}
        </div>
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
            v-for="observation in statement.observations"
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
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { Badge } from "@/components/ui/badge";
import Table from "@/components/ui/table/Table.vue";
import TableBody from "@/components/ui/table/TableBody.vue";
import TableCell from "@/components/ui/table/TableCell.vue";
import TableHead from "@/components/ui/table/TableHead.vue";
import TableHeader from "@/components/ui/table/TableHeader.vue";
import TableRow from "@/components/ui/table/TableRow.vue";
import {
  type ApplicationSQL,
  ApplicationSQLKind,
} from "@/types/proto-es/v1/application_service_pb";
import { formatDateTime } from "@/utils/datetime";

/**
 * What a reported statement has actually done: how it was classified, how often it ran,
 * when it was first and last seen, which applications reported it, and the fingerprint
 * that identifies it.
 *
 * It is a component because two surfaces show these exact numbers — the statement's row
 * on the Application SQL page and the statement's own metadata page — and they have to
 * agree. The numbers are read per statement rather than mirrored into the registry row:
 * they change with every report, and the registry history is kept forever.
 */
const props = defineProps<{
  statement: ApplicationSQL;
}>();

const { t, locale } = useI18n();

const kindLabel = computed(() => {
  switch (props.statement.kind) {
    case ApplicationSQLKind.APPLICATION_SQL_KIND_WRITE:
      return t("applicationSql.write");
    case ApplicationSQLKind.APPLICATION_SQL_KIND_READ:
      return t("applicationSql.read");
    case ApplicationSQLKind.APPLICATION_SQL_KIND_UNKNOWN:
      return t("applicationSql.unknownKind");
    default:
      return "-";
  }
});
</script>
