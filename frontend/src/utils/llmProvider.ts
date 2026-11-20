// ExplainSQL may be restricted to an admin-configured set of provider profiles
// (the workspace profile setting's allowed_llm_provider_profiles). An empty
// allowlist means every enabled profile is allowed. Only ExplainSQL applies it:
// the data quality analyses and the quality agents choose their profile in their
// own settings.
export function isProviderAllowed(
  profileName: string,
  allowedProfiles: readonly string[]
): boolean {
  return allowedProfiles.length === 0 || allowedProfiles.includes(profileName);
}

/** One selectable provider profile: it enables at least one model. */
export interface ProviderOption {
  name: string;
  title: string;
}

interface ProviderProfileLike {
  name: string;
  title: string;
  models: ReadonlyArray<{ name: string; enabled: boolean }>;
}

/**
 * The profiles a page that picks its own profile may offer: those that enable a
 * model. Kept in one helper so the data quality settings and the agent dialog cannot
 * disagree about which profiles are usable. The ExplainSQL allowlist is applied by
 * that page separately with `isProviderAllowed`.
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
