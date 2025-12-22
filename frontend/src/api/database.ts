import { create } from "@bufbuild/protobuf";
import { ListDatabaseRequestSchema } from "@/types/proto-es/v1/database_service_pb";
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
