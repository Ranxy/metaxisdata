import type { MessageInitShape } from "@bufbuild/protobuf";
import { create } from "@bufbuild/protobuf";
import {
  CreateAPIKeyRequestSchema,
  CreateNamespaceMappingRequestSchema,
  DeleteNamespaceMappingRequestSchema,
  GetOpenLineageDatasetRequestSchema,
  GetOpenLineageRunRequestSchema,
  GetOpenLineageTaskRequestSchema,
  ListAPIKeysRequestSchema,
  ListNamespaceMappingsRequestSchema,
  ListOpenLineageDatasetsRequestSchema,
  ListOpenLineageRunsRequestSchema,
  ListOpenLineageTasksRequestSchema,
  NamespaceMappingSchema,
  RevokeAPIKeyRequestSchema,
  UpdateNamespaceMappingRequestSchema,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { openLineageClient } from "./client";

export async function listNamespaceMappings() {
  const request = create(ListNamespaceMappingsRequestSchema, {});
  return await openLineageClient.listNamespaceMappings(request);
}

export async function listOpenLineageTasks(params?: {
  pageSize?: number;
  pageToken?: string;
  jobNamespace?: string;
  jobName?: string;
  jobType?: string;
  lineageOnly?: boolean;
}) {
  const request = create(ListOpenLineageTasksRequestSchema, {
    pageSize: params?.pageSize ?? 100,
    pageToken: params?.pageToken ?? "",
    jobNamespace: params?.jobNamespace ?? "",
    jobName: params?.jobName ?? "",
    jobType: params?.jobType ?? "TASK",
    lineageOnly: params?.lineageOnly ?? true,
  });
  return await openLineageClient.listOpenLineageTasks(request);
}

/** getOpenLineageTask reads one task by its guid (openlineage/tasks/{guid}). */
export async function getOpenLineageTask(guid: string) {
  const request = create(GetOpenLineageTaskRequestSchema, {
    name: `openlineage/tasks/${guid}`,
  });
  return await openLineageClient.getOpenLineageTask(request);
}

export async function listOpenLineageDatasets(params?: {
  pageSize?: number;
  pageToken?: string;
  search?: string;
  namespace?: string;
  integration?: string;
  source?: string;
  datasetScope?: number;
  columnLineageOnly?: boolean;
}) {
  const request = create(ListOpenLineageDatasetsRequestSchema, {
    pageSize: params?.pageSize ?? 200,
    pageToken: params?.pageToken ?? "",
    search: params?.search ?? "",
    namespace: params?.namespace ?? "",
    integration: params?.integration ?? "",
    source: params?.source ?? "",
    datasetScope: params?.datasetScope ?? 0,
    columnLineageOnly: params?.columnLineageOnly ?? false,
  });
  return await openLineageClient.listOpenLineageDatasets(request);
}

export async function getOpenLineageDataset(guid: string) {
  const request = create(GetOpenLineageDatasetRequestSchema, { guid });
  return await openLineageClient.getOpenLineageDataset(request);
}

export async function listOpenLineageRuns(params?: {
  pageSize?: number;
  pageToken?: string;
  jobNamespace?: string;
  jobName?: string;
  taskGuid?: string;
  jobType?: string;
  eventType?: string;
  hasLineage?: boolean;
}) {
  const request = create(ListOpenLineageRunsRequestSchema, {
    pageSize: params?.pageSize ?? 100,
    pageToken: params?.pageToken ?? "",
    jobNamespace: params?.jobNamespace ?? "",
    jobName: params?.jobName ?? "",
    taskGuid: params?.taskGuid ?? "",
    jobType: params?.jobType ?? "",
    eventType: params?.eventType ?? "",
    hasLineage: params?.hasLineage ?? false,
  });
  return await openLineageClient.listOpenLineageRuns(request);
}

/** getOpenLineageRun reads one run by its guid (openlineage/runs/{guid}). */
export async function getOpenLineageRun(guid: string) {
  const request = create(GetOpenLineageRunRequestSchema, {
    name: `openlineage/runs/${guid}`,
  });
  return await openLineageClient.getOpenLineageRun(request);
}

export async function createNamespaceMapping(mapping: {
  namespace: string;
  instanceResourceId: string;
  databaseName?: string;
}) {
  const request = create(CreateNamespaceMappingRequestSchema, {
    mapping: create(NamespaceMappingSchema, mapping),
  });
  return await openLineageClient.createNamespaceMapping(request);
}

export async function updateNamespaceMapping(
  name: string,
  mapping: MessageInitShape<typeof NamespaceMappingSchema>
) {
  const request = create(UpdateNamespaceMappingRequestSchema, {
    mapping: create(NamespaceMappingSchema, { ...mapping, name }),
  });
  return await openLineageClient.updateNamespaceMapping(request);
}

export async function deleteNamespaceMapping(name: string) {
  const request = create(DeleteNamespaceMappingRequestSchema, { name });
  return await openLineageClient.deleteNamespaceMapping(request);
}

export async function listAPIKeys() {
  const request = create(ListAPIKeysRequestSchema, {});
  return await openLineageClient.listAPIKeys(request);
}

export async function createAPIKey(description: string, scopeNamespace = "") {
  const request = create(CreateAPIKeyRequestSchema, {
    description,
    scopeNamespace,
  });
  return await openLineageClient.createAPIKey(request);
}

export async function revokeAPIKey(name: string) {
  const request = create(RevokeAPIKeyRequestSchema, { name });
  return await openLineageClient.revokeAPIKey(request);
}
