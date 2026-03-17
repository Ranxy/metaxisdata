import { create } from "@bufbuild/protobuf";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { GetLineageForContextRequestSchema } from "@/types/proto-es/v1/lineage_service_pb";
import { lineageClient } from "./client";

export async function getLineageForContext(options: {
  guid: string;
  metaType: MetaType;
}) {
  const request = create(GetLineageForContextRequestSchema, {
    guid: options.guid,
    metaType: options.metaType,
  });
  return await lineageClient.getLineageForContext(request);
}
