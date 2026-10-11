import { ref } from "vue";
import * as authApi from "@/api/auth";
import { getWorkspaceProfileSetting } from "@/api/setting";
import { beginSSOLogin, type SSOFlowMode } from "@/lib/oauth";
import type { IdentityProvider } from "@/types/proto-es/v1/identity_provider_service_pb";

/**
 * Starts an OAuth2 flow: issue the server nonce, build the authorization URL, and
 * leave the page. It is shared by the login page and the provider test button
 * because the two differ only in the `mode` stored with the flow.
 *
 * `start` resolves false when the provider is unusable — a broken configuration,
 * or a browser without WebCrypto — so the caller can say so instead of navigating
 * somewhere unexpected. Transport errors are rethrown.
 */
export function useSSOLogin() {
  const isStarting = ref(false);

  async function start(
    provider: IdentityProvider,
    options: { mode?: SSOFlowMode; redirect?: string } = {}
  ): Promise<boolean> {
    if (isStarting.value) {
      return false;
    }
    isStarting.value = true;
    try {
      // The redirect URI has to be the one the server builds for the token
      // exchange, which comes from the workspace external URL rather than the
      // address this tab happens to be on.
      const setting = await getWorkspaceProfileSetting();
      const serverState = await authApi.createSSOState();
      return await beginSSOLogin(provider, {
        serverState,
        mode: options.mode,
        redirect: options.redirect,
        externalUrl: setting.externalUrl,
      });
    } finally {
      isStarting.value = false;
    }
  }

  return { isStarting, start };
}
