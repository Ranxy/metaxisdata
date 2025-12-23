<template>
  <div>
    <div
      v-if="isLoading"
      class="p-8 flex justify-center"
    >
      <AppLoading />
    </div>

    <div
      v-else-if="instances.length === 0"
      class="p-8 text-center text-muted-foreground"
    >
      <Database class="h-12 w-12 mx-auto mb-4 text-muted-foreground/50" />
      <p>{{ t("metadataBrowser.noInstances") }}</p>
    </div>

    <Table v-else>
      <TableHeader>
        <TableRow>
          <TableHead>{{ t("metadataBrowser.instance") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.engine") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.host") }}</TableHead>
          <TableHead>{{ t("metadataBrowser.status") }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow
          v-for="instance in instances"
          :key="instance.name"
          class="cursor-pointer hover:bg-muted/50"
          @click="$emit('select', instance)"
        >
          <TableCell>
            <div class="flex items-center gap-3">
              <div
                class="w-10 h-10 rounded-full flex items-center justify-center"
                :class="getEngineBgClass(instance.engine)"
              >
                <span
                  class="font-semibold text-sm"
                  :class="getEngineTextClass(instance.engine)"
                >
                  {{ getEngineIcon(instance.engine) }}
                </span>
              </div>
              <div>
                <div class="font-medium">{{ instance.title }}</div>
                <div class="text-sm text-muted-foreground">
                  {{ getInstanceId(instance.name) }}
                </div>
              </div>
            </div>
          </TableCell>
          <TableCell>
            <Badge variant="secondary">{{ getEngineLabel(instance.engine) }}</Badge>
          </TableCell>
          <TableCell class="text-muted-foreground">
            {{ getHostInfo(instance) }}
          </TableCell>
          <TableCell>
            <Badge :variant="instance.activation ? 'success' : 'secondary'">
              {{
                instance.activation
                  ? t("metadataBrowser.active")
                  : t("metadataBrowser.inactive")
              }}
            </Badge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  </div>
</template>

<script setup lang="ts">
import { Database } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import AppLoading from "@/components/common/AppLoading.vue";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Engine } from "@/types/proto-es/v1/common_pb";
import {
  DataSourceType,
  type Instance,
} from "@/types/proto-es/v1/instance_service_pb";

defineProps<{
  instances: Instance[];
  isLoading: boolean;
}>();

defineEmits<{
  select: [instance: Instance];
}>();

const { t } = useI18n();

function getInstanceId(name: string): string {
  return name.replace("instances/", "");
}

function getHostInfo(instance: Instance): string {
  const adminDataSource = instance.dataSources.find(
    (ds) => ds.type === DataSourceType.ADMIN
  );
  if (adminDataSource) {
    const port = adminDataSource.port ? `:${adminDataSource.port}` : "";
    return `${adminDataSource.host}${port}`;
  }
  return "-";
}

function getEngineLabel(engine: Engine): string {
  const labels: Partial<Record<Engine, string>> = {
    [Engine.MYSQL]: "MySQL",
    [Engine.POSTGRES]: "PostgreSQL",
    [Engine.MSSQL]: "SQL Server",
    [Engine.ORACLE]: "Oracle",
  };
  return labels[engine] || "Unknown";
}

function getEngineIcon(engine: Engine): string {
  const icons: Partial<Record<Engine, string>> = {
    [Engine.MYSQL]: "My",
    [Engine.POSTGRES]: "PG",
    [Engine.MSSQL]: "MS",
    [Engine.ORACLE]: "OR",
  };
  return icons[engine] || "DB";
}

function getEngineBgClass(engine: Engine): string {
  const classes: Partial<Record<Engine, string>> = {
    [Engine.MYSQL]: "bg-orange-100",
    [Engine.POSTGRES]: "bg-blue-100",
    [Engine.MSSQL]: "bg-blue-100",
  };
  return classes[engine] || "bg-muted";
}

function getEngineTextClass(engine: Engine): string {
  const classes: Partial<Record<Engine, string>> = {
    [Engine.MYSQL]: "text-orange-600",
    [Engine.POSTGRES]: "text-blue-600",
    [Engine.MSSQL]: "text-blue-600",
  };
  return classes[engine] || "text-muted-foreground";
}
</script>
