import { create } from "@bufbuild/protobuf";
import {
  ListDatabaseRequestSchema,
  ListMetadataRequestSchema,
  type MetaType,
} from "@/types/proto-es/v1/database_service_pb";
import { databaseClient } from "./client";

export async function listDatabases(options: {
  parent: string;
  pageSize?: number;
  pageToken?: string;
  filter?: string;
  showDeleted?: boolean;
}) {
  const request = create(ListDatabaseRequestSchema, {
    parent: options.parent,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    filter: options.filter ?? "",
    showDeleted: options.showDeleted ?? false,
  });
  return await databaseClient.listDatabase(request);
}

/**
 * List stored metadata under a parent GUID.
 *
 * Notes:
 * - If metaType is omitted, backend ignores pageSize and returns first 20 per type.
 * - If metaType is set, nextPageToken can be used for pagination.
 */
export async function listMetadata(options: {
  parentGuid: string;
  pageSize?: number;
  pageToken?: string;
  metaType?: MetaType;
}) {
  const request = create(ListMetadataRequestSchema, {
    parentGuid: options.parentGuid,
    pageSize: options.pageSize ?? 20,
    pageToken: options.pageToken ?? "",
    metaType: options.metaType,
  });
  return await databaseClient.listMetadata(request);
}
