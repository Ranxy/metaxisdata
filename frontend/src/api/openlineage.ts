import type { MessageInitShape } from "@bufbuild/protobuf";
import { create } from "@bufbuild/protobuf";
import {
  CreateAPIKeyRequestSchema,
  CreateNamespaceMappingRequestSchema,
  DeleteNamespaceMappingRequestSchema,
  ListAPIKeyRequestSchema,
  ListNamespaceMappingRequestSchema,
  NamespaceMappingResourceSchema,
  RevokeAPIKeyRequestSchema,
  UpdateNamespaceMappingRequestSchema,
} from "@/types/proto-es/v1/openlineage_service_pb";
import { openLineageClient } from "./client";

export async function listNamespaceMapping() {
  const request = create(ListNamespaceMappingRequestSchema, {});
  return await openLineageClient.listNamespaceMapping(request);
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
