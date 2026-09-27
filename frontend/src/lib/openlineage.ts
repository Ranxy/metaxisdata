export interface OpenLineageDatasetRef {
  namespace: string;
  name: string;
}

export interface AggregatedOpenLineageDataset extends OpenLineageDatasetRef {
  runCount: number;
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

/**
 * Reports an event whose producer attached a SQL facet but no dataset at all.
 *
 * That is what a SQL extractor leaves behind when it cannot resolve the
 * statement - the run records zero inputs and zero outputs, so the event looks
 * like "this job has no lineage" instead of "the lineage was dropped".
 */
export function hasOpenLineageUnparsedSQL(rawPayload: string): boolean {
  const payload = parseOpenLineagePayload(rawPayload);
  if (!payload) {
    return false;
  }

  const job = payload.job;
  if (!job || typeof job !== "object") {
    return false;
  }

  const facets = (job as Record<string, unknown>).facets;
  if (!facets || typeof facets !== "object" || !("sql" in facets)) {
    return false;
  }

  return (
    normalizeDatasetList(payload.inputs).length === 0 &&
    normalizeDatasetList(payload.outputs).length === 0
  );
}

export interface OpenLineageExtractionError {
  task: string;
  message: string;
}

/**
 * Returns the statements the producer's extractor could not parse.
 *
 * A SQL extractor answers a statement it cannot parse with an `extractionError`
 * run facet instead of datasets, so the statement contributes no lineage and
 * this facet is the only trace of it. A run can therefore look like a job with
 * less lineage than its SQL really has.
 */
export function extractOpenLineageExtractionErrors(
  rawPayload: string
): OpenLineageExtractionError[] {
  const payload = parseOpenLineagePayload(rawPayload);
  const run = payload?.run;
  if (!run || typeof run !== "object") {
    return [];
  }

  const facets = (run as Record<string, unknown>).facets;
  if (!facets || typeof facets !== "object") {
    return [];
  }

  const facet = (facets as Record<string, unknown>).extractionError;
  if (!facet || typeof facet !== "object") {
    return [];
  }

  const errors = (facet as Record<string, unknown>).errors;
  if (!Array.isArray(errors)) {
    return [];
  }

  return errors
    .map((item) => {
      if (!item || typeof item !== "object") {
        return null;
      }

      const record = item as Record<string, unknown>;
      const task = typeof record.task === "string" ? record.task : "";
      const message =
        typeof record.errorMessage === "string" ? record.errorMessage : "";
      if (!task && !message) {
        return null;
      }

      return { task, message };
    })
    .filter((error): error is OpenLineageExtractionError => error !== null);
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
