import { create } from "@bufbuild/protobuf";
import { DurationSchema, FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import type { Engine } from "@/types/proto-es/v1/common_pb";
import type { Instance } from "@/types/proto-es/v1/instance_service_pb";
import {
  CreateDataSourceRequestSchema,
  CreateInstanceRequestSchema,
  DataSourceSchema,
  DataSourceType,
  DeleteDataSourceRequestSchema,
  DeleteInstanceRequestSchema,
  GetInstanceRequestSchema,
  InstanceSchema,
  ListInstancesRequestSchema,
  SyncInstanceRequestSchema,
  UndeleteInstanceRequestSchema,
  UpdateDataSourceRequestSchema,
  UpdateInstanceRequestSchema,
} from "@/types/proto-es/v1/instance_service_pb";
import { instanceClient } from "./client";

/** The ID of a data source, the last segment of its resource name. */
export function dataSourceId(name: string): string {
  return name.split("/").pop() ?? name;
}

/** The resource name of a data source of the given instance. */
export function dataSourceName(instanceName: string, id: string): string {
  return `${instanceName}/dataSources/${id}`;
}

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
  syncIntervalSeconds?: number;
  /** Test the data source connections on the server without creating anything. */
  validateOnly?: boolean;
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
      // The server names the data source instances/{instance}/dataSources/{id};
      // on create the client composes it so the ID it asked for is kept.
      name: input.instanceId
        ? dataSourceName(`instances/${input.instanceId}`, ds.id)
        : "",
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
    ...(input.syncIntervalSeconds
      ? {
          syncInterval: create(DurationSchema, {
            seconds: BigInt(input.syncIntervalSeconds),
          }),
        }
      : {}),
  });

  const request = create(CreateInstanceRequestSchema, {
    instance,
    instanceId: input.instanceId ?? "",
    validateOnly: input.validateOnly ?? false,
  });

  return await instanceClient.createInstance(request);
}

export async function deleteInstance(name: string) {
  const request = create(DeleteInstanceRequestSchema, {
    name,
  });
  return await instanceClient.deleteInstance(request);
}

export async function undeleteInstance(name: string) {
  const request = create(UndeleteInstanceRequestSchema, { name });
  return await instanceClient.undeleteInstance(request);
}

export async function syncInstance(name: string, enableFullSync = false) {
  const request = create(SyncInstanceRequestSchema, {
    name,
    enableFullSync,
  });
  return await instanceClient.syncInstance(request);
}

/** DataSourcePatch carries the fields to write for one data source. */
export interface DataSourcePatch {
  username?: string;
  password?: string;
  host?: string;
  port?: string;
  database?: string;
}

/**
 * createDataSource adds a read-only data source to an instance. With
 * validateOnly the server only tests the connection and stores nothing, which
 * is how a data source that does not exist yet is tested.
 */
export async function createDataSource(
  parent: string,
  dataSource: Omit<DataSourceInput, "id"> & { id?: string },
  options?: { validateOnly?: boolean }
) {
  const request = create(CreateDataSourceRequestSchema, {
    parent,
    dataSourceId: dataSource.id ?? "",
    dataSource: create(DataSourceSchema, {
      type: dataSource.type,
      username: dataSource.username,
      password: dataSource.password,
      host: dataSource.host,
      port: dataSource.port,
      database: dataSource.database ?? "",
    }),
    validateOnly: options?.validateOnly ?? false,
  });
  return await instanceClient.createDataSource(request);
}

/**
 * updateDataSource writes exactly the fields named in updateMask. Fields left
 * out keep their stored value, which is how an edit that does not carry a
 * password avoids clearing one.
 *
 * With validateOnly the server only tests the resulting connection and stores
 * nothing. An empty mask therefore tests the stored connection as-is, which is
 * what an untouched data source needs.
 */
export async function updateDataSource(
  name: string,
  patch: DataSourcePatch,
  updateMask: string[],
  options?: { validateOnly?: boolean }
) {
  const request = create(UpdateDataSourceRequestSchema, {
    dataSource: create(DataSourceSchema, { name, ...patch }),
    updateMask: create(FieldMaskSchema, { paths: updateMask }),
    validateOnly: options?.validateOnly ?? false,
  });
  return await instanceClient.updateDataSource(request);
}

/** deleteDataSource removes a read-only data source from an instance. */
export async function deleteDataSource(name: string) {
  const request = create(DeleteDataSourceRequestSchema, { name });
  return await instanceClient.deleteDataSource(request);
}

export interface UpdateInstanceInput {
  instance: Instance;
  updateMask: string[];
}

export async function updateInstance(input: UpdateInstanceInput) {
  const request = create(UpdateInstanceRequestSchema, {
    instance: input.instance,
    updateMask: create(FieldMaskSchema, { paths: input.updateMask }),
  });
  return await instanceClient.updateInstance(request);
}
