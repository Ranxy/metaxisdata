import { onMounted, ref } from "vue";
import { type BuildInfo, fetchBuildInfo } from "@/api/version";

/** How far the build metadata request has got. */
export type BuildInfoState = "loading" | "ready" | "failed";

/**
 * Reads the server's build metadata once, when the calling component mounts.
 *
 * `state` exists so a caller can tell "not answered yet" from "will never
 * answer": `buildInfo` alone stays `null` for both, and a caller that renders
 * a loading placeholder for the second case would show it forever.
 */
export function useBuildInfo() {
  const buildInfo = ref<BuildInfo | null>(null);
  const buildInfoState = ref<BuildInfoState>("loading");

  onMounted(async () => {
    buildInfo.value = await fetchBuildInfo();
    buildInfoState.value = buildInfo.value ? "ready" : "failed";
  });

  return { buildInfo, buildInfoState };
}
