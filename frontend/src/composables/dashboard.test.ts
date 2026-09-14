import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  type Database,
  DatabaseSchema,
} from "@/types/proto-es/v1/database_service_pb";
import {
  type Instance,
  InstanceResourceSchema,
  InstanceSchema,
} from "@/types/proto-es/v1/instance_service_pb";
import {
  OpenLineageDatasetResourceSchema,
  OpenLineageTaskSchema,
} from "@/types/proto-es/v1/openlineage_service_pb";
import {
  countDatabasesByInstance,
  instanceIDOfDatabase,
  summarizeEstate,
  summarizeOpenLineage,
} from "./dashboard";

function instance(
  id: string,
  options: { activation?: boolean; lastSyncSeconds?: number } = {}
): Instance {
  return create(InstanceSchema, {
    name: `instances/${id}`,
    activation: options.activation ?? true,
    lastSyncTime: options.lastSyncSeconds
      ? { seconds: BigInt(options.lastSyncSeconds), nanos: 0 }
      : undefined,
  });
}

function database(
  instanceID: string,
  name: string,
  options: { drifted?: boolean; syncSeconds?: number } = {}
): Database {
  return create(DatabaseSchema, {
    name: `instances/${instanceID}/databases/${name}`,
    drifted: options.drifted ?? false,
    successfulSyncTime: options.syncSeconds
      ? { seconds: BigInt(options.syncSeconds), nanos: 0 }
      : undefined,
    instanceResource: create(InstanceResourceSchema, {
      name: `instances/${instanceID}`,
    }),
  });
}

describe("summarizeEstate", () => {
  it("counts instances, databases and the states worth flagging", () => {
    const summary = summarizeEstate(
      [
        instance("prod", { lastSyncSeconds: 1_700_000_000 }),
        instance("staging", { activation: false }),
        instance("dev"),
      ],
      [
        database("prod", "shop", { syncSeconds: 1_700_000_000 }),
        database("prod", "billing", { drifted: true }),
        database("dev", "sandbox", { syncSeconds: 1_700_000_000 }),
      ]
    );

    expect(summary).toEqual({
      instanceCount: 3,
      activeInstanceCount: 2,
      inactiveInstanceCount: 1,
      // Only the active "dev" instance has never synced; the inactive one is
      // expected not to have synced.
      neverSyncedInstanceCount: 1,
      databaseCount: 3,
      driftedDatabaseCount: 1,
      neverSyncedDatabaseCount: 1,
    });
  });

  it("treats a zero timestamp as never synced", () => {
    const summary = summarizeEstate([instance("zero")], []);
    expect(summary.neverSyncedInstanceCount).toBe(1);
  });
});

describe("countDatabasesByInstance", () => {
  it("groups databases by the instance they belong to", () => {
    const counts = countDatabasesByInstance([
      database("prod", "shop"),
      database("prod", "billing"),
      database("dev", "sandbox"),
    ]);

    expect(counts).toEqual({ prod: 2, dev: 1 });
  });

  it("falls back to the resource name when the instance resource is absent", () => {
    const withoutResource = create(DatabaseSchema, {
      name: "instances/legacy/databases/old",
    });

    expect(instanceIDOfDatabase(withoutResource)).toBe("legacy");
    expect(countDatabasesByInstance([withoutResource])).toEqual({ legacy: 1 });
  });
});

describe("summarizeOpenLineage", () => {
  it("counts jobs, lineage-ready jobs, namespaces and dataset flags", () => {
    const summary = summarizeOpenLineage(
      [
        create(OpenLineageTaskSchema, {
          jobNamespace: "airflow",
          lineageRunCount: 2,
        }),
        create(OpenLineageTaskSchema, {
          jobNamespace: "airflow",
          lineageRunCount: 0,
        }),
        create(OpenLineageTaskSchema, {
          jobNamespace: "dbt",
          lineageRunCount: 5,
        }),
      ],
      [
        create(OpenLineageDatasetResourceSchema, {
          internal: true,
          supportsColumnLineage: true,
        }),
        create(OpenLineageDatasetResourceSchema, {
          internal: false,
          supportsColumnLineage: false,
        }),
      ]
    );

    expect(summary).toEqual({
      jobCount: 3,
      lineageReadyJobCount: 2,
      namespaceCount: 2,
      datasetCount: 2,
      internalDatasetCount: 1,
      columnLineageDatasetCount: 1,
    });
  });

  it("returns zeroes for an empty estate", () => {
    expect(summarizeOpenLineage([], [])).toEqual({
      jobCount: 0,
      lineageReadyJobCount: 0,
      namespaceCount: 0,
      datasetCount: 0,
      internalDatasetCount: 0,
      columnLineageDatasetCount: 0,
    });
  });
});
