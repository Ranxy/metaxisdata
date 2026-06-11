import { create } from "@bufbuild/protobuf";
import { ExplainSQLRequestSchema } from "@/types/proto-es/v1/explain_sql_service_pb";
import { explainSQLClient } from "./client";

export interface ExplainSQLInput {
  sqlText?: string;
  metaGuid?: string;
  metaType?: number;
  forceRegenerate?: boolean;
  providerName?: string;
  scopePrefix?: string;
}

export function explainSQL(input: ExplainSQLInput) {
  const request = create(ExplainSQLRequestSchema, {
    sqlText: input.sqlText ?? "",
    metaGuid: input.metaGuid ?? "",
    metaType: input.metaType ?? 0,
    forceRegenerate: input.forceRegenerate ?? false,
    providerName: input.providerName ?? "",
    scopePrefix: input.scopePrefix ?? "",
  });
  return explainSQLClient.explainSQL(request);
}
