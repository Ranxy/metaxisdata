import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { ApplicationService } from "@/types/proto-es/v1/application_service_pb";
import { AuditLogService } from "@/types/proto-es/v1/audit_log_service_pb";
import { AuthService } from "@/types/proto-es/v1/auth_service_pb";
import { ConnectedAppService } from "@/types/proto-es/v1/connected_app_service_pb";
import { DataQualityService } from "@/types/proto-es/v1/data_quality_service_pb";
import { DatabaseService } from "@/types/proto-es/v1/database_service_pb";
import { EnvironmentService } from "@/types/proto-es/v1/environment_service_pb";
import { ExplainSQLService } from "@/types/proto-es/v1/explain_sql_service_pb";
import { GroupService } from "@/types/proto-es/v1/group_service_pb";
import { IamService } from "@/types/proto-es/v1/iam_service_pb";
import { IdentityProviderService } from "@/types/proto-es/v1/identity_provider_service_pb";
import { InstanceService } from "@/types/proto-es/v1/instance_service_pb";
import { LineageService } from "@/types/proto-es/v1/lineage_service_pb";
import { LLMService } from "@/types/proto-es/v1/llm_service_pb";
import { NotificationService } from "@/types/proto-es/v1/notification_service_pb";
import { OAuthService } from "@/types/proto-es/v1/oauth_service_pb";
import { OpenLineageService } from "@/types/proto-es/v1/openlineage_service_pb";
import { RoleService } from "@/types/proto-es/v1/role_service_pb";
import { SettingService } from "@/types/proto-es/v1/setting_service_pb";
import { UserService } from "@/types/proto-es/v1/user_service_pb";
import { createRequestDeadline, requestTimeoutMs } from "@/utils/apiTimeout";
import { sessionInterceptor } from "./session";

const baseUrl = import.meta.env.VITE_API_BASE_URL || "";

/** The RPC path a fetch is for, e.g. "/metaxisdata.v1.DatabaseService/ListDatabases". */
function requestPath(input: RequestInfo | URL): string {
  if (typeof input === "string") {
    return input;
  }
  return input instanceof URL ? input.toString() : input.url;
}

const transport = createConnectTransport({
  baseUrl,
  interceptors: [sessionInterceptor],
  // A hung RPC must not leave a page spinning forever; LLM, explanation and
  // sync paths opt out because they legitimately outlive the default ceiling.
  fetch: async (input, init) => {
    const deadline = createRequestDeadline(
      init?.signal,
      requestTimeoutMs(requestPath(input))
    );
    try {
      return await fetch(input, {
        ...init,
        credentials: "include",
        signal: deadline.signal,
      });
    } catch (error) {
      // A fetch aborted by our deadline arrives as a DOM abort, which Connect
      // would flatten to Canceled — the code the UI reads as "the caller left".
      // Report the actual failure instead.
      if (deadline.timedOut()) {
        throw new ConnectError("The request timed out.", Code.DeadlineExceeded);
      }
      throw error;
    } finally {
      deadline.cleanup();
    }
  },
});

export const applicationClient = createClient(ApplicationService, transport);
export const authClient = createClient(AuthService, transport);
export const auditLogClient = createClient(AuditLogService, transport);
export const connectedAppClient = createClient(ConnectedAppService, transport);
export const userClient = createClient(UserService, transport);
export const instanceClient = createClient(InstanceService, transport);
export const environmentClient = createClient(EnvironmentService, transport);
export const databaseClient = createClient(DatabaseService, transport);
export const dataQualityClient = createClient(DataQualityService, transport);
export const lineageClient = createClient(LineageService, transport);
export const openLineageClient = createClient(OpenLineageService, transport);
export const llmClient = createClient(LLMService, transport);
export const notificationClient = createClient(NotificationService, transport);
export const explainSQLClient = createClient(ExplainSQLService, transport);
export const settingClient = createClient(SettingService, transport);
export const roleClient = createClient(RoleService, transport);
export const groupClient = createClient(GroupService, transport);
export const iamClient = createClient(IamService, transport);
export const oauthClient = createClient(OAuthService, transport);
export const identityProviderClient = createClient(
  IdentityProviderService,
  transport
);
