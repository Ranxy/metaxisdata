import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  type ApplicationSQL,
  ApplicationSQLKind,
  ApplicationSQLObservationSchema,
  ApplicationSQLSchema,
} from "@/types/proto-es/v1/application_service_pb";
import ApplicationSQLFacts from "./ApplicationSQLFacts.vue";

const FINGERPRINT =
  "eb2e4913504be09be52508ce1304e8a49b5e3314c5faa98c82b7889001fe2684";

function statement(): ApplicationSQL {
  return create(ApplicationSQLSchema, {
    guid: "test-pg-1;metaxisdata;public;__application_sql__/mWjmPAyBvd7O1wSy",
    sqlText: "INSERT INTO explain_sql_cache (id, cache_key) VALUES (?, ?)",
    kind: ApplicationSQLKind.APPLICATION_SQL_KIND_WRITE,
    fingerprint: FINGERPRINT,
    fingerprintVersion: 1,
    executionCount: 3n,
    firstSeenAt: timestampFromDate(new Date("2026-10-12T10:43:56Z")),
    lastSeenAt: timestampFromDate(new Date("2026-10-12T10:43:56Z")),
    observations: [
      create(ApplicationSQLObservationSchema, {
        application: "applications/7",
        applicationDisplayName: "pg-reporter",
        executionCount: 3n,
        lastSeenAt: timestampFromDate(new Date("2026-10-12T10:43:56Z")),
      }),
    ],
  });
}

function mountFacts(value: ApplicationSQL) {
  return mount(ApplicationSQLFacts, {
    props: { statement: value },
    global: { plugins: [i18n] },
  });
}

// The suite runs against the English catalog, like the other component tests.
describe("ApplicationSQLFacts", () => {
  it("labels every figure it shows", () => {
    const text = mountFacts(statement()).text();

    for (const label of [
      "Kind",
      "Runs",
      "First seen",
      "Last seen",
      "Fingerprint",
    ]) {
      expect(text).toContain(label);
    }
    expect(text).toContain("Write");
    expect(text).toContain(FINGERPRINT);
    expect(text).toContain("pg-reporter");
  });

  // The fingerprint used to be a bare hash between the reporter table and the
  // definition: nothing on the page said what it was.
  it("shows the fingerprint under its own label", () => {
    const wrapper = mountFacts(statement());
    const labelled = wrapper
      .findAll("div")
      .filter((node) => node.text().includes(FINGERPRINT));

    expect(labelled.length).toBeGreaterThan(0);
    expect(labelled[0].text()).toContain("Fingerprint");
  });

  it("reads a read-only statement without inventing a write", () => {
    const value = statement();
    value.kind = ApplicationSQLKind.APPLICATION_SQL_KIND_READ;
    const text = mountFacts(value).text();

    expect(text).toContain("Read");
    expect(text).not.toContain("Write");
  });
});
