<template>
  <div class="space-y-3">
    <div class="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
      <div>
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.kind") }}
        </div>
        <div>
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
      </div>
      <div>
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.executions") }}
        </div>
        <div class="tabular-nums">{{ statement.executionCount }}</div>
      </div>
      <div>
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.firstSeen") }}
        </div>
        <div>{{ formatDateTime(statement.firstSeenAt, locale) }}</div>
      </div>
      <div>
        <div class="text-xs text-muted-foreground">
          {{ t("applicationSql.lastSeen") }}
        </div>
        <div>{{ formatDateTime(statement.lastSeenAt, locale) }}</div>
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

    <div class="font-mono text-xs text-muted-foreground">
      {{ statement.fingerprint }}
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
 * when it was first and last seen, and which applications reported it.
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
