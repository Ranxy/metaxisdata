import { create } from "@bufbuild/protobuf";
import {
  CreateIdentityProviderRequestSchema,
  DeleteIdentityProviderBindingRequestSchema,
  DeleteIdentityProviderRequestSchema,
  GetIdentityProviderRequestSchema,
  type IdentityProvider,
  type IdentityProviderBinding,
  type IdentityProviderUserInfo,
  ListIdentityProviderBindingsRequestSchema,
  ListIdentityProvidersRequestSchema,
  TestIdentityProviderRequestSchema,
  UpdateIdentityProviderRequestSchema,
} from "@/types/proto-es/v1/identity_provider_service_pb";
import { identityProviderClient } from "./client";

/**
 * The id inside an `idps/{id}` resource name. The settings table shows it because
 * it is what the workspace setting `ssoEmailIdentityIdps` refers to, and because
 * re-creating a provider under the same id is what makes the accounts bound to it
 * reachable again.
 */
export function identityProviderId(name: string): string {
  return name.startsWith("idps/") ? name.slice("idps/".length) : name;
}

export async function listIdentityProviders(): Promise<IdentityProvider[]> {
  const response = await identityProviderClient.listIdentityProviders(
    create(ListIdentityProvidersRequestSchema, {})
  );
  return response.identityProviders;
}

export async function getIdentityProvider(
  name: string
): Promise<IdentityProvider> {
  return await identityProviderClient.getIdentityProvider(
    create(GetIdentityProviderRequestSchema, { name })
  );
}

export async function createIdentityProvider(
  identityProviderId: string,
  identityProvider: IdentityProvider
): Promise<IdentityProvider> {
  return await identityProviderClient.createIdentityProvider(
    create(CreateIdentityProviderRequestSchema, {
      identityProviderId,
      identityProvider,
    })
  );
}

export async function updateIdentityProvider(
  identityProvider: IdentityProvider,
  updateMask: string[]
): Promise<IdentityProvider> {
  return await identityProviderClient.updateIdentityProvider(
    create(UpdateIdentityProviderRequestSchema, {
      identityProvider,
      updateMask: { paths: updateMask },
    })
  );
}

export async function deleteIdentityProvider(name: string): Promise<void> {
  await identityProviderClient.deleteIdentityProvider(
    create(DeleteIdentityProviderRequestSchema, { name })
  );
}

/**
 * Completes a real authorization-code exchange against a stored provider. The
 * caller obtains `code` by sending an administrator's browser through the
 * provider with the state record marked as a test.
 */
export async function testIdentityProvider(
  name: string,
  code: string,
  codeVerifier: string
): Promise<IdentityProviderUserInfo | undefined> {
  const response = await identityProviderClient.testIdentityProvider(
    create(TestIdentityProviderRequestSchema, {
      name,
      context: {
        case: "oauth2Context",
        value: { code, codeVerifier },
      },
    })
  );
  return response.userInfo;
}

export async function listIdentityProviderBindings(filter: {
  idp?: string;
  user?: string;
}): Promise<IdentityProviderBinding[]> {
  const response = await identityProviderClient.listIdentityProviderBindings(
    create(ListIdentityProviderBindingsRequestSchema, {
      idp: filter.idp ?? "",
      user: filter.user ?? "",
    })
  );
  return response.identityProviderBindings;
}

export async function deleteIdentityProviderBinding(
  user: string,
  idp: string,
  subject: string
): Promise<void> {
  await identityProviderClient.deleteIdentityProviderBinding(
    create(DeleteIdentityProviderBindingRequestSchema, { user, idp, subject })
  );
}
