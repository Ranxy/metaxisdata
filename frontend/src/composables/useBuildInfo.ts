import { onMounted, ref } from "vue";

/**
 * The build metadata `GET /api/version` reports. It describes the server the
 * SPA was served by, which is what an operator comparing a running deployment
 * against a release needs — not the bundle's own build.
 */
export interface BuildInfo {
  version: string;
  git_commit: string;
  build_time: string;
}

/**
 * Reads the server's build metadata once, when the calling component mounts.
 *
 * The endpoint is anonymous and the display is decorative, so nothing here
 * throws or toasts: a server predating the route, or a proxy that forwards only
 * `/v1`, leaves `buildInfo` empty and the caller renders nothing.
 */
export function useBuildInfo() {
  const baseUrl = import.meta.env.VITE_API_BASE_URL || "";
  const buildInfo = ref<BuildInfo | null>(null);

  onMounted(async () => {
    try {
      const response = await fetch(`${baseUrl}/api/version`);
      if (!response.ok) {
        return;
      }
      buildInfo.value = (await response.json()) as BuildInfo;
    } catch {
      // Decorative metadata: a failure must not surface as an error.
    }
  });

  return { buildInfo };
}
