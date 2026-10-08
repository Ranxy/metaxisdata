/** The prefix of a persisted OpenLineage run GUID. */
const RUN_PREFIX = "openlineage:run:";

/** The job and run a persisted OpenLineage run GUID names. */
export interface OpenLineageRunRef {
  jobName: string;
  runID: string;
}

/**
 * Parses a persisted run GUID (`openlineage:run:<scope>:<job>:<runID>`) into the
 * job and the run id, both percent-decoded. A GUID that does not name a run
 * returns null rather than a guess.
 */
export function parseOpenLineageRun(guid: string): OpenLineageRunRef | null {
  if (!guid.startsWith(RUN_PREFIX)) {
    return null;
  }

  const segments = guid
    .substring(RUN_PREFIX.length)
    .split(":")
    .map((segment) => decodeURIComponent(segment));

  if (segments.length < 3) {
    return null;
  }
  return {
    jobName: segments[segments.length - 2],
    runID: segments[segments.length - 1],
  };
}

/**
 * The job and run a persisted run names — `dwd_load · 01a0e120…` — so a relation
 * can say which run wrote it without showing a raw GUID. A GUID that is not a run
 * is returned unchanged rather than mangled.
 */
export function openlineageRunLabel(guid: string): string {
  const run = parseOpenLineageRun(guid);
  return run ? `${run.jobName} · ${run.runID}` : guid;
}

/**
 * Just the job of a persisted run, which is what a table cell has room for; the
 * run id belongs in a tooltip. A GUID that is not a run is returned unchanged.
 */
export function openlineageJobName(guid: string): string {
  return parseOpenLineageRun(guid)?.jobName ?? guid;
}
