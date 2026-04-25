import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  CreateManualSQLRequestSchema,
  DeleteManualSQLRequestSchema,
  GetManualSQLRequestSchema,
  GetMetadataRequestSchema,
  GetSchemaStringRequestSchema,
  ListDatabaseRequestSchema,
  ListManualSQLRequestSchema,
  ListMetadataRequestSchema,
  type ManualSQL,
  ManualSQLSchema,
  type MetaType,
  SearchManualSQLRequestSchema,
  SearchMetadataRequestSchema,
  SyncDatabaseRequestSchema,
  UpdateManualSQLRequestSchema,
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

export async function getMetadata(options: {
  guid: string;
  metaType: MetaType;
}) {
  const request = create(GetMetadataRequestSchema, {
    guid: options.guid,
    metaType: options.metaType,
  });
  return await databaseClient.getMetadata(request);
}

/**
 * Get schema DDL string for a database object (table, view, materialized view).
 */
export async function getSchemaString(options: {
  guid: string;
  metaType: MetaType;
}) {
  const request = create(GetSchemaStringRequestSchema, {
    guid: options.guid,
    metaType: options.metaType,
  });
  return await databaseClient.getSchemaString(request);
}

export async function searchMetadata(options: {
  searchStr: string;
  parentGuidPrefix?: string;
  metaType?: MetaType;
}) {
  const request = create(SearchMetadataRequestSchema, {
    searchStr: options.searchStr,
    parentGuidPrefix: options.parentGuidPrefix,
    metaType: options.metaType,
  });
  return await databaseClient.searchMetadata(request);
}

export async function syncDatabase(name: string) {
  const request = create(SyncDatabaseRequestSchema, { name });
  return await databaseClient.syncDatabase(request);
}

export interface ManualSQLInput {
  name?: string;
  guid?: string;
  title?: string;
  schemaName?: string;
  comment?: string;
  sqlText: string;
  tags?: string[];
  attributes?: Record<string, string>;
}

function buildManualSQLResource(input: ManualSQLInput): ManualSQL {
  return create(ManualSQLSchema, {
    name: input.name ?? "",
    guid: input.guid ?? "",
    title: input.title ?? "",
    schemaName: input.schemaName ?? "",
    comment: input.comment ?? "",
    sqlText: input.sqlText,
    tags: input.tags ?? [],
    attributes: input.attributes ?? {},
  });
}

export async function createManualSQL(options: {
  parent: string;
  manualSqlId: string;
  manualSql: ManualSQLInput;
}) {
  const request = create(CreateManualSQLRequestSchema, {
    parent: options.parent,
    manualSqlId: options.manualSqlId,
    manualSql: buildManualSQLResource(options.manualSql),
  });
  return await databaseClient.createManualSQL(request);
}

export async function getManualSQL(name: string) {
  const request = create(GetManualSQLRequestSchema, { name });
  return await databaseClient.getManualSQL(request);
}

export async function listManualSQL(options: {
  parent: string;
  pageSize?: number;
  pageToken?: string;
  schemaName?: string;
  tags?: string[];
  showDeleted?: boolean;
}) {
  const request = create(ListManualSQLRequestSchema, {
    parent: options.parent,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    schemaName: options.schemaName ?? "",
    tags: options.tags ?? [],
    showDeleted: options.showDeleted ?? false,
  });
  return await databaseClient.listManualSQL(request);
}

export async function searchManualSQL(options: {
  parent: string;
  query: string;
  pageSize?: number;
  pageToken?: string;
  schemaName?: string;
  tags?: string[];
}) {
  const request = create(SearchManualSQLRequestSchema, {
    parent: options.parent,
    query: options.query,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    schemaName: options.schemaName ?? "",
    tags: options.tags ?? [],
  });
  return await databaseClient.searchManualSQL(request);
}

export async function updateManualSQL(options: {
  manualSql: ManualSQLInput & { name: string };
  updateMask?: string[];
}) {
  const request = create(UpdateManualSQLRequestSchema, {
    manualSql: buildManualSQLResource(options.manualSql),
    updateMask: create(FieldMaskSchema, {
      paths: options.updateMask ?? [
        "title",
        "schema_name",
        "comment",
        "sql_text",
        "tags",
        "attributes",
      ],
    }),
  });
  return await databaseClient.updateManualSQL(request);
}

export async function deleteManualSQL(name: string) {
  const request = create(DeleteManualSQLRequestSchema, { name });
  return await databaseClient.deleteManualSQL(request);
}
