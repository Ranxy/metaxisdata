import { create } from "@bufbuild/protobuf";
import type { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type {
  ExternalDatasetInfo,
  LineageRelation,
} from "@/types/proto-es/v1/lineage_service_pb";
import {
  GetLineageCountsRequestSchema,
  GetLineageRequestSchema,
  LineageType,
} from "@/types/proto-es/v1/lineage_service_pb";
import { lineageClient } from "./client";

/** The server refuses a batch larger than this; see maxLineageCountGuids. */
const LINEAGE_COUNT_BATCH = 200;

/** One object's degree in the stored graph. */
export type LineageCount = {
  guid: string;
  upstream: number;
  downstream: number;
};

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

/**
 * getLineageCounts returns the degree of each given object: how many distinct
 * objects it is connected to, upstream and downstream.
 *
 * This is what labels a graph's nodes. A node's degree is not derivable from
 * its neighbours, so asking one object at a time would mean downloading every
 * relation of every node just to print two numbers; the server counts them in
 * two aggregates instead. Batches are split so one call cannot exceed the
 * server's ceiling, and every requested guid comes back, zero-filled.
 */
export async function getLineageCounts(
  guids: string[]
): Promise<Map<string, LineageCount>> {
  const counts = new Map<string, LineageCount>();

  for (let start = 0; start < guids.length; start += LINEAGE_COUNT_BATCH) {
    const request = create(GetLineageCountsRequestSchema, {
      guids: guids.slice(start, start + LINEAGE_COUNT_BATCH),
    });
    const response = await lineageClient.getLineageCounts(request);
    for (const count of response.counts) {
      counts.set(count.guid, {
        guid: count.guid,
        upstream: count.upstreamCount,
        downstream: count.downstreamCount,
      });
    }
  }

  return counts;
}
