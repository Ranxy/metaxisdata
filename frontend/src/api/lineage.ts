import { create } from "@bufbuild/protobuf";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  GetLineageRequestSchema,
  LineageType,
} from "@/types/proto-es/v1/lineage_service_pb";
import { lineageClient } from "./client";

export async function getLineage(options: {
  guid: string;
  metaType: MetaType;
  lineageType?: LineageType;
}) {
  const request = create(GetLineageRequestSchema, {
    guid: options.guid,
    metaType: options.metaType,
    lineageType: options.lineageType ?? LineageType.LINEAGE_TYPE_UNSPECIFIED,
  });
  return await lineageClient.getLineage(request);
}
