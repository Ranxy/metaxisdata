import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AuthService } from "@/types/proto-es/v1/auth_service_pb";
import { DatabaseService } from "@/types/proto-es/v1/database_service_pb";
import { InstanceService } from "@/types/proto-es/v1/instance_service_pb";
import { LineageService } from "@/types/proto-es/v1/lineage_service_pb";
import { UserService } from "@/types/proto-es/v1/user_service_pb";

const baseUrl = import.meta.env.VITE_API_BASE_URL || "";

const transport = createConnectTransport({
  baseUrl,
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
});

export const authClient = createClient(AuthService, transport);
export const userClient = createClient(UserService, transport);
export const instanceClient = createClient(InstanceService, transport);
export const databaseClient = createClient(DatabaseService, transport);
export const lineageClient = createClient(LineageService, transport);
