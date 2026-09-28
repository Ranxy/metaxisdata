import { create } from "@bufbuild/protobuf";
import {
  ApproveOAuthAuthorizationRequestRequestSchema,
  GetOAuthAuthorizationRequestRequestSchema,
} from "@/types/proto-es/v1/oauth_service_pb";
import { oauthClient } from "./client";

/**
 * oauthAuthorizationRequestName is the resource name the RPCs address a pending
 * request by. The id is opaque and assigned by the server, so it is passed
 * through untouched.
 */
export function oauthAuthorizationRequestName(requestId: string): string {
  return `oauthAuthorizationRequests/${requestId}`;
}

/** Reads one pending request for the consent page. Display data only. */
export async function getOAuthAuthorizationRequest(requestId: string) {
  const request = create(GetOAuthAuthorizationRequestRequestSchema, {
    name: oauthAuthorizationRequestName(requestId),
  });
  return await oauthClient.getOAuthAuthorizationRequest(request);
}

/**
 * Records the signed-in user's decision. Nothing else about the request is sent
 * back: the authorization code is minted by the server at
 * /oauth/authorize/complete and never travels through this response.
 */
export async function approveOAuthAuthorizationRequest(
  requestId: string,
  approve: boolean
) {
  const request = create(ApproveOAuthAuthorizationRequestRequestSchema, {
    name: oauthAuthorizationRequestName(requestId),
    approve,
  });
  return await oauthClient.approveOAuthAuthorizationRequest(request);
}
