// ExplainSQL may be restricted to an admin-configured set of provider profiles
// (the workspace profile setting's allowed_llm_provider_profiles). An empty
// allowlist means every enabled profile is allowed.
export function isProviderAllowed(
  profileName: string,
  allowedProfiles: readonly string[]
): boolean {
  return allowedProfiles.length === 0 || allowedProfiles.includes(profileName);
}
