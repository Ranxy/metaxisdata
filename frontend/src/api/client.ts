import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AuditLogService } from "@/types/proto-es/v1/audit_log_service_pb";
import { AuthService } from "@/types/proto-es/v1/auth_service_pb";
import { DatabaseService } from "@/types/proto-es/v1/database_service_pb";
import { ExplainSQLService } from "@/types/proto-es/v1/explain_sql_service_pb";
import { GroupService } from "@/types/proto-es/v1/group_service_pb";
import { IamService } from "@/types/proto-es/v1/iam_service_pb";
import { InstanceService } from "@/types/proto-es/v1/instance_service_pb";
import { LineageService } from "@/types/proto-es/v1/lineage_service_pb";
import { LLMService } from "@/types/proto-es/v1/llm_service_pb";
import { OpenLineageService } from "@/types/proto-es/v1/openlineage_service_pb";
import { RoleService } from "@/types/proto-es/v1/role_service_pb";
import { SettingService } from "@/types/proto-es/v1/setting_service_pb";
import { UserService } from "@/types/proto-es/v1/user_service_pb";

const baseUrl = import.meta.env.VITE_API_BASE_URL || "";

const transport = createConnectTransport({
  baseUrl,
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
});

export const authClient = createClient(AuthService, transport);
export const auditLogClient = createClient(AuditLogService, transport);
export const userClient = createClient(UserService, transport);
export const instanceClient = createClient(InstanceService, transport);
export const databaseClient = createClient(DatabaseService, transport);
export const lineageClient = createClient(LineageService, transport);
export const openLineageClient = createClient(OpenLineageService, transport);
export const llmClient = createClient(LLMService, transport);
export const explainSQLClient = createClient(ExplainSQLService, transport);
export const settingClient = createClient(SettingService, transport);
export const roleClient = createClient(RoleService, transport);
export const groupClient = createClient(GroupService, transport);
export const iamClient = createClient(IamService, transport);
