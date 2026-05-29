export interface OpenLineageDatasetRef {
  namespace: string;
  name: string;
}

export interface AggregatedOpenLineageDataset extends OpenLineageDatasetRef {
  runCount: number;
}

export function toGuidPath(guid: string): string {
  return guid
    .split(";")
    .map((segment) => (segment === "" ? "~" : encodeURIComponent(segment)))
    .join("/");
}

type OpenLineagePayloadLike = {
  rawPayload: string;
};

function parseOpenLineagePayload(
  rawPayload: string
): Record<string, unknown> | null {
  if (!rawPayload) {
    return null;
  }

  try {
    return JSON.parse(rawPayload) as Record<string, unknown>;
  } catch {
    return null;
  }
}

function toDatasetKey(dataset: OpenLineageDatasetRef): string {
  return `${dataset.namespace}\u0000${dataset.name}`;
}

function getDatasetRunCount(dataset: OpenLineageDatasetRef): number | null {
  if ("runCount" in dataset && typeof dataset.runCount === "number") {
    return dataset.runCount;
  }

  return null;
}

function sortDatasets<T extends OpenLineageDatasetRef>(datasets: T[]): T[] {
  return datasets.sort((left, right) => {
    const leftCount = getDatasetRunCount(left);
    const rightCount = getDatasetRunCount(right);
    if (leftCount !== null && rightCount !== null && leftCount !== rightCount) {
      return rightCount - leftCount;
    }

    if (left.name !== right.name) {
      return left.name.localeCompare(right.name);
    }

    return left.namespace.localeCompare(right.namespace);
  });
}

function normalizeDatasetList(value: unknown): OpenLineageDatasetRef[] {
  if (!Array.isArray(value)) {
    return [];
  }

  return value
    .map((item) => {
      if (!item || typeof item !== "object") {
        return null;
      }

      const record = item as Record<string, unknown>;
      const namespace =
        typeof record.namespace === "string" ? record.namespace : "";
      const name = typeof record.name === "string" ? record.name : "";
      if (!namespace && !name) {
        return null;
      }

      return { namespace, name };
    })
    .filter((dataset): dataset is OpenLineageDatasetRef => dataset !== null);
}

export function extractOpenLineageDatasets(rawPayload: string): {
  inputs: OpenLineageDatasetRef[];
  outputs: OpenLineageDatasetRef[];
} {
  const payload = parseOpenLineagePayload(rawPayload);
  return {
    inputs: normalizeDatasetList(payload?.inputs),
    outputs: normalizeDatasetList(payload?.outputs),
  };
}

function aggregateDatasetList(
  runs: OpenLineagePayloadLike[],
  kind: "inputs" | "outputs"
): AggregatedOpenLineageDataset[] {
  const datasetMap = new Map<string, AggregatedOpenLineageDataset>();

  for (const run of runs) {
    const datasets = extractOpenLineageDatasets(run.rawPayload)[kind];
    const seenInRun = new Set<string>();

    for (const dataset of datasets) {
      const key = toDatasetKey(dataset);
      if (seenInRun.has(key)) {
        continue;
      }
      seenInRun.add(key);

      const current = datasetMap.get(key);
      if (current) {
        current.runCount += 1;
        continue;
      }

      datasetMap.set(key, {
        ...dataset,
        runCount: 1,
      });
    }
  }

  return sortDatasets(Array.from(datasetMap.values()));
}

export function aggregateOpenLineageDatasets(runs: OpenLineagePayloadLike[]): {
  inputs: AggregatedOpenLineageDataset[];
  outputs: AggregatedOpenLineageDataset[];
} {
  return {
    inputs: aggregateDatasetList(runs, "inputs"),
    outputs: aggregateDatasetList(runs, "outputs"),
  };
}
