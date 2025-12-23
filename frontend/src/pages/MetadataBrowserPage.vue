<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-bold tracking-tight">
        {{ t("metadataBrowser.title") }}
      </h1>
      <p class="text-muted-foreground">
        {{ t("metadataBrowser.description") }}
      </p>
    </div>

    <Card>
      <CardContent class="py-3 px-4">
        <MetadataBreadcrumb
          :path-segments="breadcrumbSegments"
          @navigate="handleNavigate"
        />
      </CardContent>
    </Card>

    <Card>
      <div
        v-if="isLoading"
        class="p-8 flex justify-center"
      >
        <AppLoading />
      </div>

      <div
        v-else-if="error"
        class="p-8 text-center text-destructive"
      >
        {{ error }}
      </div>

      <div v-else-if="isRootPath">
        <CardHeader>
          <CardTitle>{{ t("metadataBrowser.instances") }}</CardTitle>
        </CardHeader>
        <InstanceList
          :instances="instances"
          :is-loading="isLoadingInstances"
          @select="handleSelectInstance"
        />
      </div>

      <div v-else>
        <CardHeader class="border-b">
          <MetadataTabNav
            :groups="metadataGroups"
            :active="activeMetaType"
            @select="handleSelectTab"
          />
        </CardHeader>

        <CardContent class="p-0">
          <MetadataList
            v-if="activeGroup"
            :meta-type="activeGroup.metaType"
            :items="activeGroup.list"
            :current-guid="currentGuid"
            :is-mysql="isMySQLInstance"
            @select="handleSelectMetadata"
          />
          <div
            v-else
            class="p-8 text-center text-muted-foreground"
          >
            {{ t("metadataBrowser.empty") }}
          </div>
        </CardContent>

        <div
          v-if="selectedMetaType && nextPageToken"
          class="p-4 border-t"
        >
          <MetadataPagination
            :has-next="!!nextPageToken"
            :is-loading="isLoadingMore"
            @load-more="loadMoreMetadata"
          />
        </div>
      </div>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { listMetadata } from "@/api/database";
import { getInstance, listInstances } from "@/api/instance";
import AppLoading from "@/components/common/AppLoading.vue";
import InstanceList from "@/components/metadata/InstanceList.vue";
import MetadataBreadcrumb from "@/components/metadata/MetadataBreadcrumb.vue";
import MetadataList from "@/components/metadata/MetadataList.vue";
import MetadataPagination from "@/components/metadata/MetadataPagination.vue";
import MetadataTabNav from "@/components/metadata/MetadataTabNav.vue";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Engine, State } from "@/types/proto-es/v1/common_pb";
import {
  type MetadataResponse_MetadataList,
  MetaType,
  type StoredMetadata,
} from "@/types/proto-es/v1/database_service_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import { extractErrorMessage } from "@/utils/error";

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const isLoading = ref(false);
const isLoadingInstances = ref(false);
const isLoadingMore = ref(false);
const error = ref<string | null>(null);

const instances = ref<Instance[]>([]);
const metadataGroups = ref<MetadataResponse_MetadataList[]>([]);

const activeMetaType = ref<MetaType | null>(null);
const selectedMetaType = ref<MetaType | null>(null);

// Pagination is only valid when metaType is specified.
const nextPageToken = ref("");

const currentInstanceEngine = ref<Engine | null>(null);

const currentGuid = computed(() => {
  const guidParam = route.params.guid;
  if (!guidParam) return "";

  const guidStr = Array.isArray(guidParam) ? guidParam.join("/") : guidParam;
  const segments = guidStr
    .split("/")
    .map((s) => decodeURIComponent(s))
    .map((s) => (s === "~" ? "" : s));

  return segments.join(";");
});

const isRootPath = computed(() => !currentGuid.value);

const guidSegments = computed(() => {
  if (!currentGuid.value) return [];
  // keep empty string segment (MySQL empty schema)
  return currentGuid.value.split(";");
});

const instanceIdFromGuid = computed(() => {
  const first = guidSegments.value[0];
  return first || "";
});

const breadcrumbSegments = computed(() => {
  // Breadcrumb display should not show trailing empty segment.
  const segments = guidSegments.value;
  if (segments.length === 0) return [];
  const trimmed = [...segments];
  while (trimmed.length > 0 && trimmed[trimmed.length - 1] === "") {
    trimmed.pop();
  }
  return trimmed;
});

const isMySQLInstance = computed(() => {
  if (!currentInstanceEngine.value) return false;
  return (
    currentInstanceEngine.value === Engine.MYSQL ||
    currentInstanceEngine.value === Engine.MARIADB ||
    currentInstanceEngine.value === Engine.TIDB
  );
});

const activeGroup = computed(() => {
  if (!activeMetaType.value) return null;
  return (
    metadataGroups.value.find((g) => g.metaType === activeMetaType.value) ??
    null
  );
});

function getEffectiveParentGuidForListing(): string {
  // MySQL-family special case: objects are under an "empty schema".
  // The backend expects the parent guid to include the trailing semicolon
  // (e.g. "instances/x;databases/y;") to list tables/views directly.
  const base = currentGuid.value;
  if (!base) return "";
  if (!isMySQLInstance.value) return base;
  if (guidSegments.value.length === 2 && !base.endsWith(";")) {
    return `${base};`;
  }
  return base;
}

async function fetchInstances() {
  isLoadingInstances.value = true;
  error.value = null;

  try {
    const response = await listInstances({ pageSize: 100 });
    instances.value = response.instances.filter(
      (i) => i.state !== State.DELETED
    );
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoadingInstances.value = false;
  }
}

async function fetchCurrentInstanceEngineIfNeeded() {
  if (!instanceIdFromGuid.value) return;
  if (currentInstanceEngine.value) return;

  try {
    const inst = await getInstance(`instances/${instanceIdFromGuid.value}`);
    currentInstanceEngine.value = inst.engine;
  } catch {
    // Non-fatal: engine detection only affects MySQL empty schema handling.
  }
}

async function fetchMetadataGroups() {
  isLoading.value = true;
  error.value = null;

  try {
    await fetchCurrentInstanceEngineIfNeeded();

    const response = await listMetadata({
      parentGuid: getEffectiveParentGuidForListing(),
      pageSize: 20,
    });

    metadataGroups.value = response.typesStoredMetadata;

    if (!activeMetaType.value && response.typesStoredMetadata.length > 0) {
      activeMetaType.value = response.typesStoredMetadata[0].metaType;
    }

    // When metaType is omitted, nextPageToken is not meaningful.
    nextPageToken.value = "";
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function fetchMetaTypeFirstPage(metaType: MetaType) {
  isLoading.value = true;
  error.value = null;

  try {
    const response = await listMetadata({
      parentGuid: getEffectiveParentGuidForListing(),
      pageSize: 20,
      pageToken: "",
      metaType,
    });

    const returned = response.typesStoredMetadata[0];
    if (returned) {
      const group = metadataGroups.value.find((g) => g.metaType === metaType);
      if (group) {
        group.list = [...returned.list];
      } else {
        // Fall back to response payload (has correct protobuf message shape).
        metadataGroups.value = response.typesStoredMetadata;
      }
    }

    nextPageToken.value = response.nextPageToken;
    selectedMetaType.value = metaType;
  } catch (e) {
    const msg = extractErrorMessage(e);
    error.value = msg || t("metadataBrowser.fetchError");
  } finally {
    isLoading.value = false;
  }
}

async function loadMoreMetadata() {
  if (!selectedMetaType.value) return;
  if (!nextPageToken.value) return;

  isLoadingMore.value = true;

  try {
    const response = await listMetadata({
      parentGuid: getEffectiveParentGuidForListing(),
      pageSize: 20,
      pageToken: nextPageToken.value,
      metaType: selectedMetaType.value,
    });

    const returned = response.typesStoredMetadata[0];
    if (returned) {
      const group = metadataGroups.value.find(
        (g) => g.metaType === selectedMetaType.value
      );
      if (group) {
        group.list.push(...returned.list);
      }
    }

    nextPageToken.value = response.nextPageToken;
  } finally {
    isLoadingMore.value = false;
  }
}

function toGuidPath(segments: string[]): string {
  return segments
    .map((s) => (s === "" ? "~" : encodeURIComponent(s)))
    .join("/");
}

function handleNavigate(index: number) {
  if (index < 0) {
    router.push({ name: "MetadataBrowser" });
    return;
  }

  const segments = breadcrumbSegments.value.slice(0, index + 1);
  router.push({
    name: "MetadataDetail",
    params: { guid: toGuidPath(segments) },
  });
}

function handleSelectInstance(instance: Instance) {
  const instanceId = instance.name.replace("instances/", "");
  currentInstanceEngine.value = instance.engine;
  router.push({
    name: "MetadataDetail",
    params: { guid: toGuidPath([instanceId]) },
  });
}

function getMetadataName(item: StoredMetadata): string {
  switch (item.type.case) {
    case "databaseSchemaMetadata":
    case "schemaMetadata":
    case "tableMetadata":
    case "viewMetadata":
    case "externalTableMetadata":
    case "materializedViewMetadata":
    case "functionMetadata":
    case "procedureMetadata":
    case "packageMetadata":
    case "sequenceMetadata":
    case "streamMetadata":
    case "taskMetadata":
      return item.type.value.name;
    default:
      return "";
  }
}

function handleSelectMetadata(item: StoredMetadata, metaType: MetaType) {
  const name = getMetadataName(item);
  const newSegments = [...guidSegments.value];

  // guidSegments keeps empty schema if already there.
  while (newSegments.length > 0 && newSegments[newSegments.length - 1] === "") {
    newSegments.pop();
  }

  // If we're at MySQL database-level (instance;database) but listing children of
  // the implicit empty schema, we must include the empty schema segment in the URL
  // so subsequent navigation has a correct guid.
  if (
    isMySQLInstance.value &&
    guidSegments.value.length === 2 &&
    metaType !== MetaType.DATABASE
  ) {
    newSegments.push("");
  }

  newSegments.push(name);

  // MySQL special: when navigating from database -> children, add empty schema.
  if (metaType === MetaType.DATABASE && isMySQLInstance.value) {
    newSegments.push("");
  }

  router.push({
    name: "MetadataDetail",
    params: { guid: toGuidPath(newSegments) },
  });
}

async function handleSelectTab(metaType: MetaType) {
  activeMetaType.value = metaType;

  // Selecting a tab enables pagination for that metaType.
  // Always fetch the first page with metaType to obtain a valid nextPageToken.
  await fetchMetaTypeFirstPage(metaType);
}

watch(
  () => route.params.guid,
  async () => {
    activeMetaType.value = null;
    selectedMetaType.value = null;
    nextPageToken.value = "";
    currentInstanceEngine.value = null;

    if (isRootPath.value) {
      await fetchInstances();
    } else {
      await fetchMetadataGroups();
    }
  },
  { immediate: true }
);

onMounted(async () => {
  if (isRootPath.value) {
    await fetchInstances();
  } else {
    await fetchMetadataGroups();
  }
});
</script>
