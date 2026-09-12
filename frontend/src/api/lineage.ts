import { create } from "@bufbuild/protobuf";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type {
  ExternalDatasetInfo,
  LineageRelation,
} from "@/types/proto-es/v1/lineage_service_pb";
import {
  GetLineageRequestSchema,
  LineageType,
} from "@/types/proto-es/v1/lineage_service_pb";
import { lineageClient } from "./client";

/** The whole lineage of one object, gathered across every page. */
export type Lineage = {
  relationsSource: LineageRelation[];
  relationsTarget: LineageRelation[];
  externalDatasets: ExternalDatasetInfo[];
};

/**
 * getLineage returns every lineage relation of an object.
 *
 * The server caps each list at page_size (500 by default), so the pages are
 * walked and merged here. The same offset applies to the source and the target
 * list, so an individual page may carry only one of them.
 */
export async function getLineage(options: {
  guid: string;
  metaType: MetaType;
  lineageType?: LineageType;
}): Promise<Lineage> {
  const relationsSource: LineageRelation[] = [];
  const relationsTarget: LineageRelation[] = [];
  const externalDatasets = new Map<string, ExternalDatasetInfo>();

  let pageToken = "";
  for (;;) {
    const request = create(GetLineageRequestSchema, {
      guid: options.guid,
      metaType: options.metaType,
      lineageType: options.lineageType ?? LineageType.LINEAGE_TYPE_UNSPECIFIED,
      pageToken,
    });
    const response = await lineageClient.getLineage(request);

    relationsSource.push(...response.relationsSource);
    relationsTarget.push(...response.relationsTarget);
    for (const dataset of response.externalDatasets) {
      externalDatasets.set(dataset.guid, dataset);
    }

    // Stop on the last page, and never follow a token that does not advance.
    const nextPageToken = response.nextPageToken;
    if (!nextPageToken || nextPageToken === pageToken) {
      break;
    }
    pageToken = nextPageToken;
  }

  return {
    relationsSource,
    relationsTarget,
    externalDatasets: [...externalDatasets.values()],
  };
}
