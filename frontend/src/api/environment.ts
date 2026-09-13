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
import { environmentClient } from "./client";

const ENVIRONMENT_PREFIX = "environments/";

/** The resource name of an environment from its id. */
export function environmentName(id: string): string {
  return `${ENVIRONMENT_PREFIX}${id}`;
}

/** The id of an environment, the last segment of its resource name. */
export function environmentId(name: string): string {
  if (!name) return "";
  return name.startsWith(ENVIRONMENT_PREFIX)
    ? name.slice(ENVIRONMENT_PREFIX.length)
    : name;
}

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
