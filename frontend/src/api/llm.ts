import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import type { LlmProviderProfile } from "@/types/proto-es/v1/llm_service_pb";
import {
  CreateLLMProviderProfileRequestSchema,
  DeleteLLMProviderProfileRequestSchema,
  FetchLLMModelsRequestSchema,
  ListLLMProviderProfilesRequestSchema,
  LLMProviderType,
  UpdateLLMProviderProfileRequestSchema,
} from "@/types/proto-es/v1/llm_service_pb";
import { llmClient } from "./client";

export async function listProfiles(options?: {
  pageSize?: number;
  pageToken?: string;
}) {
  const request = create(ListLLMProviderProfilesRequestSchema, {
    pageSize: options?.pageSize ?? 50,
    pageToken: options?.pageToken ?? "",
  });
  return await llmClient.listLLMProviderProfiles(request);
}

export async function createProfile(profile: LlmProviderProfile) {
  const request = create(CreateLLMProviderProfileRequestSchema, { profile });
  return await llmClient.createLLMProviderProfile(request);
}

export async function updateProfile(
  profile: LlmProviderProfile,
  updateMask: string[]
) {
  const request = create(UpdateLLMProviderProfileRequestSchema, {
    profile,
    updateMask: create(FieldMaskSchema, { paths: updateMask }),
  });
  return await llmClient.updateLLMProviderProfile(request);
}

export async function deleteProfile(name: string) {
  const request = create(DeleteLLMProviderProfileRequestSchema, { name });
  return await llmClient.deleteLLMProviderProfile(request);
}

export async function fetchModelsByKey(
  providerType: LLMProviderType,
  apiKey: string
) {
  const request = create(FetchLLMModelsRequestSchema, {
    providerType,
    apiKey,
  });
  return await llmClient.fetchLLMModels(request);
}

export async function fetchModelsByProfile(name: string) {
  const request = create(FetchLLMModelsRequestSchema, { name });
  return await llmClient.fetchLLMModels(request);
}
