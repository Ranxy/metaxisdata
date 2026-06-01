import type { MessageInitShape } from "@bufbuild/protobuf";
import { create } from "@bufbuild/protobuf";
import {
  CreateAPIKeyRequestSchema,
  CreateNamespaceMappingRequestSchema,
  DeleteNamespaceMappingRequestSchema,
  GetOpenLineageDatasetRequestSchema,
  GetOpenLineageRunRequestSchema,
  GetOpenLineageTaskRequestSchema,
  ListAPIKeyRequestSchema,
  ListNamespaceMappingRequestSchema,
  ListOpenLineageDatasetsRequestSchema,
  ListOpenLineageRunsRequestSchema,
  ListOpenLineageTasksRequestSchema,
  NamespaceMappingResourceSchema,
  RevokeAPIKeyRequestSchema,
  UpdateNamespaceMappingRequestSchema,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { openLineageClient } from "./client";

export async function listNamespaceMapping() {
  const request = create(ListNamespaceMappingRequestSchema, {});
  return await openLineageClient.listNamespaceMapping(request);
}

export async function listOpenLineageTasks(params?: {
  pageSize?: number;
  offset?: number;
  jobNamespace?: string;
  jobName?: string;
  jobType?: string;
  lineageOnly?: boolean;
}) {
  const request = create(ListOpenLineageTasksRequestSchema, {
    pageSize: params?.pageSize ?? 100,
    offset: params?.offset ?? 0,
    jobNamespace: params?.jobNamespace ?? "",
    jobName: params?.jobName ?? "",
    jobType: params?.jobType ?? "TASK",
    lineageOnly: params?.lineageOnly ?? true,
  });
  return await openLineageClient.listOpenLineageTasks(request);
}

export async function getOpenLineageTask(guid: string) {
  const request = create(GetOpenLineageTaskRequestSchema, { guid });
  return await openLineageClient.getOpenLineageTask(request);
}

export async function listOpenLineageDatasets(params?: {
  pageSize?: number;
  offset?: number;
  search?: string;
  namespace?: string;
  integration?: string;
  source?: string;
  datasetScope?: number;
  columnLineageOnly?: boolean;
}) {
  const request = create(ListOpenLineageDatasetsRequestSchema, {
    pageSize: params?.pageSize ?? 200,
    offset: params?.offset ?? 0,
    search: params?.search ?? "",
    namespace: params?.namespace ?? "",
    integration: params?.integration ?? "",
    source: params?.source ?? "",
    datasetScope: params?.datasetScope ?? 0,
    columnLineageOnly: params?.columnLineageOnly ?? false,
  });
  return await openLineageClient.listOpenLineageDatasets(request);
}

export async function getOpenLineageDataset(params: {
  guid: string;
  namespace: string;
  name: string;
}) {
  const request = create(GetOpenLineageDatasetRequestSchema, {
    guid: params.guid,
    namespace: params.namespace,
    name: params.name,
  });
  return await openLineageClient.getOpenLineageDataset(request);
}

export async function listOpenLineageRuns(params?: {
  pageSize?: number;
  offset?: number;
  jobNamespace?: string;
  jobName?: string;
  taskGuid?: string;
  jobType?: string;
  eventType?: string;
  hasLineage?: boolean;
}) {
  const request = create(ListOpenLineageRunsRequestSchema, {
    pageSize: params?.pageSize ?? 100,
    offset: params?.offset ?? 0,
    jobNamespace: params?.jobNamespace ?? "",
    jobName: params?.jobName ?? "",
    taskGuid: params?.taskGuid ?? "",
    jobType: params?.jobType ?? "",
    eventType: params?.eventType ?? "",
    hasLineage: params?.hasLineage ?? false,
  });
  return await openLineageClient.listOpenLineageRuns(request);
}

export async function getOpenLineageRun(guid: string) {
  const request = create(GetOpenLineageRunRequestSchema, { guid });
  return await openLineageClient.getOpenLineageRun(request);
}

export async function createNamespaceMapping(mapping: {
  namespace: string;
  instanceResourceId: string;
  databaseName?: string;
}) {
  const request = create(CreateNamespaceMappingRequestSchema, {
    mapping: create(NamespaceMappingResourceSchema, mapping),
  });
  return await openLineageClient.createNamespaceMapping(request);
}

export async function updateNamespaceMapping(
  id: bigint,
  mapping: MessageInitShape<typeof NamespaceMappingResourceSchema>
) {
  const request = create(UpdateNamespaceMappingRequestSchema, {
    id,
    mapping: create(NamespaceMappingResourceSchema, mapping),
  });
  return await openLineageClient.updateNamespaceMapping(request);
}

export async function deleteNamespaceMapping(id: bigint) {
  const request = create(DeleteNamespaceMappingRequestSchema, { id });
  return await openLineageClient.deleteNamespaceMapping(request);
}

export async function listAPIKey() {
  const request = create(ListAPIKeyRequestSchema, {});
  return await openLineageClient.listAPIKey(request);
}

export async function createAPIKey(description: string) {
  const request = create(CreateAPIKeyRequestSchema, { description });
  return await openLineageClient.createAPIKey(request);
}

export async function revokeAPIKey(id: bigint) {
  const request = create(RevokeAPIKeyRequestSchema, { id });
  return await openLineageClient.revokeAPIKey(request);
}
