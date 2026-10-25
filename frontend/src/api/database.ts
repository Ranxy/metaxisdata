import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { FieldMaskSchema, timestampFromDate } from "@bufbuild/protobuf/wkt";
import {
  CreateManualSQLRequestSchema,
  DeleteManualSQLRequestSchema,
  DiffMetadataRequestSchema,
  GetManualSQLRequestSchema,
  GetMetadataHistoryEventRequestSchema,
  GetMetadataRequestSchema,
  GetSchemaStringRequestSchema,
  ListDatabasesRequestSchema,
  ListManualSQLsRequestSchema,
  ListMetadataHistoryRequestSchema,
  ListMetadataRequestSchema,
  type ManualSQL,
  ManualSQLSchema,
  type MetadataHistoryOperation,
  type MetaType,
  SearchManualSQLRequestSchema,
  SearchMetadataRequestSchema,
  type StoredMetadata,
  SyncDatabaseRequestSchema,
  UpdateManualSQLRequestSchema,
} from "@/types/proto-es/v1/database_service_pb";
import { databaseClient } from "./client";
import { listAll } from "./list";

export async function listDatabases(options: {
  parent: string;
  pageSize?: number;
  pageToken?: string;
  filter?: string;
  showDeleted?: boolean;
  signal?: AbortSignal;
}) {
  const request = create(ListDatabasesRequestSchema, {
    parent: options.parent,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    filter: options.filter ?? "",
    showDeleted: options.showDeleted ?? false,
  });
  return await databaseClient.listDatabases(request, {
    signal: options.signal,
  });
}

/**
 * List stored metadata under a parent GUID.
 *
 * Notes:
 * - page_size applies per meta type when metaType is omitted: the response
 *   groups the rows and carries a nextPageToken on each group.
 * - With metaType set, the response holds exactly one group.
 */
export async function listMetadata(options: {
  parentGuid: string;
  pageSize?: number;
  pageToken?: string;
  metaType?: MetaType;
  signal?: AbortSignal;
}) {
  const request = create(ListMetadataRequestSchema, {
    parentGuid: options.parentGuid,
    pageSize: options.pageSize ?? 20,
    pageToken: options.pageToken ?? "",
    metaType: options.metaType,
  });
  return await databaseClient.listMetadata(request, {
    signal: options.signal,
  });
}

/**
 * Every child of one meta type under a parent, walked page by page.
 *
 * A caller that renders a picker's whole option list cannot guess a page size
 * large enough — a wide table exceeds any fixed cap, and reading only the first
 * page used to drop the rest silently. The walk is still the caller's job when
 * the list only ever grows visually (see `useMetadataChildren`); this helper is
 * for consumers that need the complete set, such as the rule form's column
 * suggestions.
 */
export async function listAllMetadata(options: {
  parentGuid: string;
  metaType: MetaType;
  pageSize?: number;
  signal?: AbortSignal;
}): Promise<StoredMetadata[]> {
  return await listAll(async (pageToken) => {
    const response = await listMetadata({
      parentGuid: options.parentGuid,
      metaType: options.metaType,
      pageSize: options.pageSize ?? 1000,
      pageToken,
      signal: options.signal,
    });
    const group = response.typesStoredMetadata.find(
      (candidate) => candidate.metaType === options.metaType
    );
    return {
      items: group?.list ?? [],
      nextPageToken: group?.nextPageToken ?? "",
    };
  });
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

export async function listMetadataHistory(options: {
  guid: string;
  metaType: MetaType;
  pageSize?: number;
  pageToken?: string;
}) {
  const request = create(ListMetadataHistoryRequestSchema, {
    guid: options.guid,
    metaType: options.metaType,
    pageSize: options.pageSize ?? 20,
    pageToken: options.pageToken ?? "",
  });
  return await databaseClient.listMetadataHistory(request);
}

export async function getMetadataHistoryEvent(options: {
  guid: string;
  metaType: MetaType;
  eventTime?: Timestamp;
  operation: MetadataHistoryOperation;
}) {
  const request = create(GetMetadataHistoryEventRequestSchema, {
    guid: options.guid,
    metaType: options.metaType,
    eventTime: options.eventTime,
    operation: options.operation,
  });
  return await databaseClient.getMetadataHistoryEvent(request);
}

/**
 * Get schema definition content for a metadata object.
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
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
}) {
  const request = create(SearchMetadataRequestSchema, {
    searchStr: options.searchStr,
    parentGuidPrefix: options.parentGuidPrefix,
    metaType: options.metaType,
    pageSize: options.pageSize ?? 0,
    pageToken: options.pageToken ?? "",
  });
  return await databaseClient.searchMetadata(request, {
    signal: options.signal,
  });
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
  signal?: AbortSignal;
}) {
  const request = create(ListManualSQLsRequestSchema, {
    parent: options.parent,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    schemaName: options.schemaName ?? "",
    tags: options.tags ?? [],
    showDeleted: options.showDeleted ?? false,
  });
  return await databaseClient.listManualSQLs(request, {
    signal: options.signal,
  });
}

export async function searchManualSQL(options: {
  parent: string;
  query: string;
  pageSize?: number;
  pageToken?: string;
  schemaName?: string;
  tags?: string[];
  signal?: AbortSignal;
}) {
  const request = create(SearchManualSQLRequestSchema, {
    parent: options.parent,
    query: options.query,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    schemaName: options.schemaName ?? "",
    tags: options.tags ?? [],
  });
  return await databaseClient.searchManualSQL(request, {
    signal: options.signal,
  });
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

export async function diffMetadata(options: {
  guid: string;
  sourceTime?: Date;
  targetTime?: Date;
}) {
  const request = create(DiffMetadataRequestSchema, {
    guid: options.guid,
    sourceTime: options.sourceTime
      ? timestampFromDate(options.sourceTime)
      : undefined,
    targetTime: options.targetTime
      ? timestampFromDate(options.targetTime)
      : undefined,
  });
  return await databaseClient.diffMetadata(request);
}
