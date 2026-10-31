import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import type { Environment } from "@/types/proto-es/v1/environment_service_pb";
import {
  CreateEnvironmentRequestSchema,
  DeleteEnvironmentRequestSchema,
  EnvironmentSchema,
  ListEnvironmentsRequestSchema,
  UpdateEnvironmentRequestSchema,
} from "@/types/proto-es/v1/environment_service_pb";
import { environmentId, environmentName } from "@/utils/environment";
import { environmentClient } from "./client";
import { listAll } from "./list";

// The pure resource-name helpers live in `utils/` (the bottom layer); this
// module re-exports them so request-building callers keep one import.
export { environmentId, environmentName };

export async function listEnvironments(options?: {
  pageSize?: number;
  pageToken?: string;
}) {
  const request = create(ListEnvironmentsRequestSchema, {
    pageSize: options?.pageSize ?? 100,
    pageToken: options?.pageToken ?? "",
  });
  return await environmentClient.listEnvironments(request);
}

/** Every environment, across all pages. */
export async function listAllEnvironments(): Promise<Environment[]> {
  return await listAll(async (pageToken) => {
    const response = await listEnvironments({ pageSize: 100, pageToken });
    return {
      items: response.environments,
      nextPageToken: response.nextPageToken,
    };
  });
}

/**
 * Create an environment. Only the title is sent: the server derives the
 * immutable id and picks a color.
 */
export async function createEnvironment(title: string, color = "") {
  const request = create(CreateEnvironmentRequestSchema, {
    environment: create(EnvironmentSchema, { title, color }),
  });
  return await environmentClient.createEnvironment(request);
}

export async function updateEnvironment(
  environment: Environment,
  updateMask: string[]
) {
  const request = create(UpdateEnvironmentRequestSchema, {
    environment,
    updateMask: create(FieldMaskSchema, { paths: updateMask }),
  });
  return await environmentClient.updateEnvironment(request);
}

export async function deleteEnvironment(name: string) {
  const request = create(DeleteEnvironmentRequestSchema, { name });
  return await environmentClient.deleteEnvironment(request);
}
