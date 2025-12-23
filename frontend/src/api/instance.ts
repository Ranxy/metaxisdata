import { create } from "@bufbuild/protobuf";
import type { Engine } from "@/types/proto-es/v1/common_pb";
import {
  CreateInstanceRequestSchema,
  DataSourceSchema,
  DataSourceType,
  DeleteInstanceRequestSchema,
  GetInstanceRequestSchema,
  InstanceSchema,
  ListInstancesRequestSchema,
  UndeleteInstanceRequestSchema,
} from "@/types/proto-es/v1/instance_service_pb";
import { instanceClient } from "./client";

export interface DataSourceInput {
  id: string;
  type: DataSourceType;
  username: string;
  password: string;
  host: string;
  port: string;
  database?: string;
}

export interface CreateInstanceInput {
  title: string;
  engine: Engine;
  environment: string;
  activation: boolean;
  dataSources: DataSourceInput[];
  instanceId?: string;
}

export async function listInstances(options?: {
  pageSize?: number;
  pageToken?: string;
  showDeleted?: boolean;
  filter?: string;
}) {
  const request = create(ListInstancesRequestSchema, {
    pageSize: options?.pageSize ?? 50,
    pageToken: options?.pageToken ?? "",
    showDeleted: options?.showDeleted ?? false,
    filter: options?.filter ?? "",
  });
  return await instanceClient.listInstances(request);
}

export async function getInstance(name: string) {
  const request = create(GetInstanceRequestSchema, { name });
  return await instanceClient.getInstance(request);
}

export async function createInstance(input: CreateInstanceInput) {
  const dataSources = input.dataSources.map((ds) =>
    create(DataSourceSchema, {
      id: ds.id,
      type: ds.type,
      username: ds.username,
      password: ds.password,
      host: ds.host,
      port: ds.port,
      database: ds.database ?? "",
    })
  );

  const instance = create(InstanceSchema, {
    title: input.title,
    engine: input.engine,
    environment: input.environment,
    activation: input.activation,
    dataSources,
  });

  const request = create(CreateInstanceRequestSchema, {
    instance,
    instanceId: input.instanceId ?? "",
  });

  return await instanceClient.createInstance(request);
}

export async function deleteInstance(name: string, force?: boolean) {
  const request = create(DeleteInstanceRequestSchema, {
    name,
    force: force ?? false,
  });
  return await instanceClient.deleteInstance(request);
}

export async function undeleteInstance(name: string) {
  const request = create(UndeleteInstanceRequestSchema, { name });
  return await instanceClient.undeleteInstance(request);
}
