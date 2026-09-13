import { environmentId } from "@/api/environment";
import type { Environment } from "@/types/proto-es/v1/environment_service_pb";

/**
 * The preset palette the server assigns from. Keys must stay in sync with
 * `environmentPalette` in backend/store/environment_mutation.go.
 */
export const ENVIRONMENT_COLOR_KEYS = [
  "slate",
  "blue",
  "green",
  "amber",
  "orange",
  "red",
  "violet",
  "pink",
] as const;

export type EnvironmentColorKey = (typeof ENVIRONMENT_COLOR_KEYS)[number];

/** Palette key → hex. Inline styles keep Tailwind from purging dynamic names. */
const ENVIRONMENT_COLOR_HEX: Record<EnvironmentColorKey, string> = {
  slate: "#64748b",
  blue: "#3b82f6",
  green: "#22c55e",
  amber: "#f59e0b",
  orange: "#f97316",
  red: "#ef4444",
  violet: "#8b5cf6",
  pink: "#ec4899",
};

/**
 * The stored color, or a stable palette entry derived from the id when the
 * server left it empty (the two seeded environments have no color).
 */
export function environmentColorKey(
  environment: Environment
): EnvironmentColorKey {
  const color = environment.color as EnvironmentColorKey;
  if (ENVIRONMENT_COLOR_KEYS.includes(color)) {
    return color;
  }
  return fallbackColor(environmentId(environment.name));
}

export function environmentColorHex(environment: Environment): string {
  return ENVIRONMENT_COLOR_HEX[environmentColorKey(environment)];
}

/** The hex for a palette key, used by the color swatches on the settings page. */
export function environmentColorHexByKey(key: EnvironmentColorKey): string {
  return ENVIRONMENT_COLOR_HEX[key];
}

function fallbackColor(id: string): EnvironmentColorKey {
  let hash = 0;
  for (let i = 0; i < id.length; i++) {
    hash = (hash * 31 + id.charCodeAt(i)) >>> 0;
  }
  return ENVIRONMENT_COLOR_KEYS[hash % ENVIRONMENT_COLOR_KEYS.length];
}
