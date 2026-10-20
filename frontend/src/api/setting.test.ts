import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { DataQualitySettingSchema } from "@/types/proto-es/v1/setting_service_pb";
import { buildUpdateDataQualitySettingRequest } from "./setting";

describe("buildUpdateDataQualitySettingRequest", () => {
  it("writes only the data_quality field", () => {
    const request = buildUpdateDataQualitySettingRequest(
      create(DataQualitySettingSchema, {
        enableLlmAnalysis: true,
        failureSampleLimit: 25,
        defaultScheduleInterval: create(DurationSchema, {
          seconds: 900n,
          nanos: 0,
        }),
      })
    );

    expect(request.updateMask?.paths).toEqual(["data_quality"]);
    expect(request.setting?.dataQuality).toMatchObject({
      enableLlmAnalysis: true,
      failureSampleLimit: 25,
    });
    expect(request.setting?.dataQuality?.defaultScheduleInterval?.seconds).toBe(
      900n
    );
    // Nothing else in the profile is sent, so the mask has nothing else to apply.
    expect(request.setting?.externalUrl).toBe("");
    expect(request.setting?.allowedLlmProviderProfiles).toEqual([]);
  });
});
