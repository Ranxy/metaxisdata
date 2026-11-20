// ExplainSQL may be restricted to an admin-configured set of (provider profile,
// model) pairs (the workspace profile setting's allowed_llm_provider_models). An
// empty allowlist means every enabled model is allowed. Only ExplainSQL applies
// it: the data quality analyses and the quality agents choose their pair in their
// own settings.

/** The resource-name prefix a provider profile is addressed by. */
const PROFILE_PREFIX = "llm-provider-profiles/";

/**
 * A profile's identity for comparison: a trimmed id, without the resource-name
 * prefix, so a stored profile reference and the one a list RPC returns compare
 * equal even if one carries the prefix and the other does not.
 */
function profileId(profile: string): string {
  const trimmed = profile.trim();
  return trimmed.startsWith(PROFILE_PREFIX)
    ? trimmed.slice(PROFILE_PREFIX.length)
    : trimmed;
}

/** One stored (provider profile, model) allowlist entry. */
export interface LlmProviderModelRefLike {
  providerProfile: string;
  model: string;
}

/**
 * Whether ExplainSQL may use this exact pair. An empty allowlist admits every
 * enabled model; otherwise the profile id and the trimmed model name must both
 * match an entry, because one profile can enable several models at different
 * prices and capacities.
 */
export function isModelAllowed(
  profile: string,
  model: string,
  allowed: readonly LlmProviderModelRefLike[]
): boolean {
  if (allowed.length === 0) {
    return true;
  }
  const id = profileId(profile);
  const name = model.trim();
  return allowed.some(
    (entry) =>
      profileId(entry.providerProfile) === id && entry.model.trim() === name
  );
}

/**
 * A stable key for one pair, for de-duplication, ordering and dirty snapshots.
 * The separator is a NUL byte because a model name may contain `/`: joining the
 * two parts into a resource-path-shaped string would let distinct pairs collide.
 */
export function modelRefKey(profile: string, model: string): string {
  return `${profileId(profile)}\u0000${model.trim()}`;
}

/** One selectable provider profile: it enables at least one model. */
export interface ProviderOption {
  name: string;
  title: string;
}

/** The shape of a provider profile this module needs to read. */
export interface ProviderProfileLike {
  name: string;
  title: string;
  models: ReadonlyArray<{ name: string; enabled: boolean }>;
}

/** One selectable model of one profile, with its profile's display title. */
export interface EnabledModelRef {
  providerProfile: string;
  model: string;
  profileTitle: string;
}

/**
 * Every enabled (profile, model) pair, grouped by profile in registry order and
 * by model inside the profile. Pages that offer a model pick the pair from here
 * and apply the ExplainSQL allowlist with `isModelAllowed`; the data quality pages
 * offer these pairs without that filter.
 */
export function enabledModelRefs(
  profiles: readonly ProviderProfileLike[]
): EnabledModelRef[] {
  const refs: EnabledModelRef[] = [];
  for (const profile of profiles) {
    const title = profile.title || profile.name;
    for (const model of profile.models) {
      if (model.enabled) {
        refs.push({
          providerProfile: profile.name,
          model: model.name,
          profileTitle: title,
        });
      }
    }
  }
  return refs;
}

/**
 * The profiles a page that picks its own profile may offer: those that enable a
 * model. Kept in one helper so the data quality settings and the agent dialog cannot
 * disagree about which profiles are usable. The ExplainSQL allowlist is applied by
 * that page separately with `isModelAllowed`, and the model is chosen with
 * `enabledModelsFor`.
 */
export function buildProviderOptions(
  profiles: readonly ProviderProfileLike[]
): ProviderOption[] {
  return profiles
    .filter((profile) => profile.models.some((model) => model.enabled))
    .map((profile) => ({
      name: profile.name,
      title: profile.title || profile.name,
    }));
}

/**
 * The models one profile currently enables, in registry order. An empty list means
 * the profile cannot run, so the agent dialog never offers it as a valid choice.
 */
export function enabledModelsFor(
  profiles: readonly ProviderProfileLike[],
  providerProfile: string
): string[] {
  const profile = profiles.find(
    (candidate) => candidate.name === providerProfile
  );
  if (!profile) {
    return [];
  }
  return profile.models
    .filter((model) => model.enabled)
    .map((model) => model.name);
}

/**
 * The display label of a stored profile reference, falling back to the reference. It
 * reads the raw profile list on purpose: a stored profile that no longer enables a
 * model still has a title worth showing next to its unavailable marker.
 */
export function profileTitle(
  name: string,
  profiles: readonly { name: string; title: string }[]
): string {
  const profile = profiles.find((candidate) => candidate.name === name);
  return profile?.title || name;
}

/** Server-owned capacity contract, mirrored from `LlmProviderLimits`. */
export interface ModelCapacityLimits {
  defaultContextWindow: number;
  minContextWindow: number;
  maxContextWindow: number;
  defaultMaxOutputTokens: number;
  minMaxOutputTokens: number;
  maxMaxOutputTokens: number;
}

/** One editable model row's text state; blank capacities mean the default. */
export interface ModelCapacityRow {
  name: string;
  enabled: boolean;
  contextWindow: string;
  maxOutputTokens: string;
}

/** One model an endpoint disclosed, with zero for an undisclosed capacity. */
export interface DiscoveredModelCapacity {
  name: string;
  contextWindow: number;
  maxOutputTokens: number;
}

/** A row-level capacity failure; the page renders it as "Model N: ...". */
export interface ModelCapacityError {
  key:
    | "modelNameRequired"
    | "modelNameDuplicate"
    | "contextWindowInvalid"
    | "modelMaxOutputTokensInvalid"
    | "modelCapacityMismatch";
  params: Record<string, number>;
}

/** Decimal capacity shorthand: 128K is 128000, 1M is 1000000. */
const CAPACITY_PATTERN = /^(\d+)([km])?$/i;
const KILO = 1_000;
const MEGA = 1_000_000;

/**
 * Parse a capacity field. Blank means "use the server default"; unreadable
 * text returns NaN so the caller can keep the user's keystrokes and report the
 * row instead of silently dropping it.
 */
export function parseTokenCapacity(text: string): number | undefined {
  const trimmed = text.trim();
  if (trimmed === "") {
    return undefined;
  }
  const match = CAPACITY_PATTERN.exec(trimmed);
  if (match === null) {
    return Number.NaN;
  }
  const suffix = match[2]?.toLowerCase();
  const scale = suffix === "k" ? KILO : suffix === "m" ? MEGA : 1;
  return Number(match[1]) * scale;
}

/**
 * Spell a stored capacity in the shortest decimal form that round-trips through
 * {@link parseTokenCapacity}; 131072 stays written out because it is not a
 * whole number of thousands.
 */
export function formatTokenCapacity(value: number): string {
  if (!Number.isInteger(value) || value <= 0) {
    return String(value);
  }
  if (value % MEGA === 0) {
    return `${value / MEGA}M`;
  }
  if (value % KILO === 0) {
    return `${value / KILO}K`;
  }
  return String(value);
}

/** Read one field: blank or 0 uses the fallback; returns undefined when invalid. */
function readCapacity(
  text: string,
  fallback: number,
  min: number,
  max: number
): number | undefined {
  const trimmed = text.trim();
  if (trimmed === "" || trimmed === "0") {
    return fallback;
  }
  const parsed = parseTokenCapacity(trimmed);
  if (
    parsed === undefined ||
    Number.isNaN(parsed) ||
    parsed < min ||
    parsed > max
  ) {
    return undefined;
  }
  return parsed;
}

/**
 * Validate every model row: unique non-empty names, in-range capacities, and a
 * positive message budget. The returned array is index-aligned with the rows.
 */
export function validateModelRows(
  rows: readonly ModelCapacityRow[],
  limits: ModelCapacityLimits
): (ModelCapacityError | undefined)[] {
  const errors: (ModelCapacityError | undefined)[] = rows.map(() => undefined);
  const seen = new Map<string, number>();
  rows.forEach((row, index) => {
    const name = row.name.trim();
    if (name === "") {
      errors[index] = { key: "modelNameRequired", params: {} };
      return;
    }
    if (seen.has(name)) {
      errors[index] = { key: "modelNameDuplicate", params: {} };
      return;
    }
    seen.set(name, index);
  });

  rows.forEach((row, index) => {
    if (errors[index] !== undefined) {
      return;
    }
    const contextWindow = readCapacity(
      row.contextWindow,
      limits.defaultContextWindow,
      limits.minContextWindow,
      limits.maxContextWindow
    );
    if (contextWindow === undefined) {
      errors[index] = {
        key: "contextWindowInvalid",
        params: { min: limits.minContextWindow, max: limits.maxContextWindow },
      };
      return;
    }
    const maxOutputTokens = readCapacity(
      row.maxOutputTokens,
      limits.defaultMaxOutputTokens,
      limits.minMaxOutputTokens,
      limits.maxMaxOutputTokens
    );
    if (maxOutputTokens === undefined) {
      errors[index] = {
        key: "modelMaxOutputTokensInvalid",
        params: {
          min: limits.minMaxOutputTokens,
          max: limits.maxMaxOutputTokens,
        },
      };
      return;
    }
    if (maxOutputTokens >= contextWindow) {
      errors[index] = { key: "modelCapacityMismatch", params: {} };
    }
  });

  return errors;
}

/**
 * Merge a fetched model listing into the editable rows. Existing rows (saved or
 * hand-added) are kept, discovered models are appended disabled, and a blank
 * capacity adopts a disclosed value only when the resulting row is still valid.
 */
export function mergeDiscoveredModels(
  rows: readonly ModelCapacityRow[],
  discovered: readonly DiscoveredModelCapacity[],
  limits: ModelCapacityLimits
): ModelCapacityRow[] {
  const merged: ModelCapacityRow[] = [];
  const byName = new Map<string, ModelCapacityRow>();
  for (const row of rows) {
    const name = row.name.trim();
    const copy = { ...row };
    if (name !== "" && !byName.has(name)) {
      byName.set(name, copy);
    }
    // Duplicate saved rows are kept so the page's row-level validation can
    // report them instead of silently dropping one with its capacities.
    merged.push(copy);
  }

  for (const model of discovered) {
    const name = model.name.trim();
    if (name === "") {
      continue;
    }
    const existing = byName.get(name);
    if (existing !== undefined) {
      const candidate = {
        ...existing,
        contextWindow:
          existing.contextWindow.trim() === ""
            ? disclosedCapacity(
                model.contextWindow,
                limits.minContextWindow,
                limits.maxContextWindow
              )
            : existing.contextWindow,
        maxOutputTokens:
          existing.maxOutputTokens.trim() === ""
            ? disclosedCapacity(
                model.maxOutputTokens,
                limits.minMaxOutputTokens,
                limits.maxMaxOutputTokens
              )
            : existing.maxOutputTokens,
      };
      const [error] = validateModelRows([candidate], limits);
      if (error === undefined) {
        existing.contextWindow = candidate.contextWindow;
        existing.maxOutputTokens = candidate.maxOutputTokens;
      }
      continue;
    }

    const candidate: ModelCapacityRow = {
      name,
      enabled: false,
      contextWindow: disclosedCapacity(
        model.contextWindow,
        limits.minContextWindow,
        limits.maxContextWindow
      ),
      maxOutputTokens: disclosedCapacity(
        model.maxOutputTokens,
        limits.minMaxOutputTokens,
        limits.maxMaxOutputTokens
      ),
    };
    const [error] = validateModelRows([candidate], limits);
    const row =
      error === undefined
        ? candidate
        : { ...candidate, contextWindow: "", maxOutputTokens: "" };
    byName.set(name, row);
    merged.push(row);
  }

  return merged;
}

/** Format a disclosed capacity, or blank when the endpoint did not disclose one. */
function disclosedCapacity(value: number, min: number, max: number): string {
  if (!Number.isInteger(value) || value < min || value > max) {
    return "";
  }
  return formatTokenCapacity(value);
}
