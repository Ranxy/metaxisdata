// ExplainSQL may be restricted to an admin-configured set of provider profiles
// (the workspace profile setting's allowed_llm_provider_profiles). An empty
// allowlist means every enabled profile is allowed.
export function isProviderAllowed(
  profileName: string,
  allowedProfiles: readonly string[]
): boolean {
  return allowedProfiles.length === 0 || allowedProfiles.includes(profileName);
}

/** One selectable provider profile: the workspace allows it and it enables a model. */
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
 * The profiles the model-backed pages may offer: an enabled model and inside the
 * workspace allowlist. Kept in one helper so the data quality settings and the agent
 * dialog cannot disagree about which profiles are usable.
 */
export function buildProviderOptions(
  profiles: readonly ProviderProfileLike[],
  allowed: readonly string[]
): ProviderOption[] {
  return profiles
    .filter((profile) => profile.models.some((model) => model.enabled))
    .filter((profile) => isProviderAllowed(profile.name, allowed))
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

/** The display label of a stored profile reference, falling back to the reference. */
export function providerLabel(
  name: string,
  options: readonly ProviderOption[]
): string {
  return options.find((option) => option.name === name)?.title ?? name;
}
