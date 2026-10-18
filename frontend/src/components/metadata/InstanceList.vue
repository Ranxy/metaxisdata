<template>
  <div>
    <PageState :loading="isLoading">
      <EmptyState
        v-if="instances.length === 0"
        :icon="Database"
        :title="t('metadataBrowser.noInstances')"
      >
        <Button
          variant="outline"
          size="sm"
          @click="$emit('refresh')"
        >
          <RefreshCw class="mr-1 h-3.5 w-3.5" />
          {{ t("common.refresh") }}
        </Button>
      </EmptyState>

      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead>{{ t("metadataBrowser.instance") }}</TableHead>
            <TableHead class="hidden sm:table-cell">{{ t("metadataBrowser.engine") }}</TableHead>
            <TableHead class="hidden md:table-cell">{{ t("metadataBrowser.host") }}</TableHead>
            <TableHead>{{ t("metadataBrowser.status") }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow
            v-for="instance in instances"
            :key="instance.name"
            class="cursor-pointer hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring focus-visible:outline-none"
            role="button"
            tabindex="0"
            @click="$emit('select', instance)"
            @keydown.enter.self.prevent="$emit('select', instance)"
            @keydown.space.self.prevent="$emit('select', instance)"
          >
            <TableCell>
              <div class="flex items-center gap-3">
                <div
                  class="w-10 h-10 rounded-full flex items-center justify-center"
                  :class="engineBgClass(instance.engine)"
                >
                  <span
                    class="font-semibold text-sm"
                    :class="engineTextClass(instance.engine)"
                  >
                    {{ engineIcon(instance.engine) }}
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
            <TableCell class="hidden sm:table-cell">
              <Badge
                variant="secondary"
                :class="engineBadgeClass(instance.engine)"
              >
                {{ engineLabel(instance.engine) }}
              </Badge>
            </TableCell>
            <TableCell class="hidden text-muted-foreground md:table-cell">
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
    </PageState>
  </div>
</template>

<script setup lang="ts">
import { Database, RefreshCw } from "lucide-vue-next";
import { useI18n } from "vue-i18n";
import EmptyState from "@/components/common/EmptyState.vue";
import PageState from "@/components/common/PageState.vue";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  DataSourceType,
  type Instance,
} from "@/types/proto-es/v1/instance_service_pb";
import {
  engineBadgeClass,
  engineBgClass,
  engineIcon,
  engineLabel,
  engineTextClass,
} from "@/utils/engine";

defineProps<{
  instances: Instance[];
  isLoading: boolean;
}>();

defineEmits<{
  select: [instance: Instance];
  refresh: [];
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
</script>
