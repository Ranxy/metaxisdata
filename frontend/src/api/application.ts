import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  ApplicationSQLKind,
  CreateApplicationAPIKeyRequestSchema,
  CreateApplicationRequestSchema,
  DeleteApplicationRequestSchema,
  DeleteApplicationSQLRequestSchema,
  GetApplicationSQLRequestSchema,
  ListApplicationAPIKeysRequestSchema,
  ListApplicationSQLsRequestSchema,
  ListApplicationsRequestSchema,
  RevokeApplicationAPIKeyRequestSchema,
  UpdateApplicationRequestSchema,
} from "@/types/proto-es/v1/application_service_pb";
import { applicationClient } from "./client";

/** listApplications pages the applications an operator manages. */
export async function listApplications(params?: {
  pageSize?: number;
  pageToken?: string;
  showDisabled?: boolean;
  signal?: AbortSignal;
}) {
  const request = create(ListApplicationsRequestSchema, {
    pageSize: params?.pageSize ?? 50,
    pageToken: params?.pageToken ?? "",
    showDisabled: params?.showDisabled ?? false,
  });
  return await applicationClient.listApplications(request, {
    signal: params?.signal,
  });
}

/**
 * createApplication registers an application and returns its first API key in plaintext.
 * The key is only ever returned here, so the caller has to show it before dismissing.
 */
export async function createApplication(params: {
  displayName: string;
  description?: string;
  apiKeyDescription?: string;
}) {
  const request = create(CreateApplicationRequestSchema, {
    application: {
      displayName: params.displayName,
      description: params.description ?? "",
    },
    apiKeyDescription: params.apiKeyDescription ?? "",
  });
  return await applicationClient.createApplication(request);
}

export async function updateApplication(params: {
  /** Format: applications/{application_id} */
  name: string;
  displayName?: string;
  description?: string;
  disabled?: boolean;
}) {
  const paths: string[] = [];
  const application: {
    displayName?: string;
    description?: string;
    disabled?: boolean;
  } = {};
  if (params.displayName !== undefined) {
    paths.push("display_name");
    application.displayName = params.displayName;
  }
  if (params.description !== undefined) {
    paths.push("description");
    application.description = params.description;
  }
  if (params.disabled !== undefined) {
    paths.push("disabled");
    application.disabled = params.disabled;
  }
  const request = create(UpdateApplicationRequestSchema, {
    application: { name: params.name, ...application },
    updateMask: create(FieldMaskSchema, { paths }),
  });
  return await applicationClient.updateApplication(request);
}

export async function deleteApplication(name: string) {
  const request = create(DeleteApplicationRequestSchema, { name });
  return await applicationClient.deleteApplication(request);
}

export async function createApplicationAPIKey(params: {
  /** Format: applications/{application_id} */
  parent: string;
  description?: string;
}) {
  const request = create(CreateApplicationAPIKeyRequestSchema, {
    parent: params.parent,
    description: params.description ?? "",
  });
  return await applicationClient.createApplicationAPIKey(request);
}

export async function listApplicationAPIKeys(params: {
  /** Format: applications/{application_id} */
  parent: string;
  signal?: AbortSignal;
}) {
  const request = create(ListApplicationAPIKeysRequestSchema, {
    parent: params.parent,
  });
  return await applicationClient.listApplicationAPIKeys(request, {
    signal: params.signal,
  });
}

export async function revokeApplicationAPIKey(name: string) {
  const request = create(RevokeApplicationAPIKeyRequestSchema, { name });
  return await applicationClient.revokeApplicationAPIKey(request);
}

/** listApplicationSQLs pages the statements reported against one instance and database. */
export async function listApplicationSQLs(params: {
  /** Format: instances/{instance}/databases/{database} */
  parent: string;
  pageSize?: number;
  pageToken?: string;
  application?: string;
  schemaName?: string;
  kind?: ApplicationSQLKind;
  query?: string;
  signal?: AbortSignal;
}) {
  const request = create(ListApplicationSQLsRequestSchema, {
    parent: params.parent,
    pageSize: params.pageSize ?? 50,
    pageToken: params.pageToken ?? "",
    application: params.application ?? "",
    schemaName: params.schemaName ?? "",
    kind: params.kind ?? ApplicationSQLKind.APPLICATION_SQL_KIND_UNSPECIFIED,
    query: params.query ?? "",
  });
  return await applicationClient.listApplicationSQLs(request, {
    signal: params.signal,
  });
}

export async function getApplicationSQL(guid: string) {
  const request = create(GetApplicationSQLRequestSchema, { guid });
  return await applicationClient.getApplicationSQL(request);
}

export async function deleteApplicationSQL(guid: string) {
  const request = create(DeleteApplicationSQLRequestSchema, { guid });
  return await applicationClient.deleteApplicationSQL(request);
}
