import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  ColumnMetadataSchema,
  type ExternalTableMetadata,
  ExternalTableMetadataSchema,
} from "@/types/proto-es/v1/database_service_pb";
import ExternalTableMetadataDetail from "./ExternalTableMetadataDetail.vue";

function table(): ExternalTableMetadata {
  return create(ExternalTableMetadataSchema, {
    name: "foreign_users",
    externalServerName: "remote_server",
    externalDatabaseName: "remote_db",
    columns: [
      create(ColumnMetadataSchema, {
        name: "user_id",
        type: "integer",
        position: 1,
      }),
      create(ColumnMetadataSchema, {
        name: "user_name",
        type: "text",
        position: 2,
        nullable: true,
      }),
    ],
  });
}

// Mounted without a guid, so neither the lineage section nor the history tab
// issues a request: this covers the column list the page renders.
function mountDetail(value: ExternalTableMetadata) {
  return mount(ExternalTableMetadataDetail, {
    props: { table: value },
    global: { plugins: [i18n] },
  });
}

describe("ExternalTableMetadataDetail", () => {
  it("names the external table and the remote objects it reads", () => {
    const wrapper = mountDetail(table());

    expect(wrapper.text()).toContain("foreign_users");
    expect(wrapper.text()).toContain("remote_server");
    expect(wrapper.text()).toContain("remote_db");
    expect(wrapper.text()).toContain("2 columns");
  });

  it("renders every column with its type and nullability", () => {
    const wrapper = mountDetail(table());

    const rows = wrapper.findAll("tbody tr");
    expect(rows).toHaveLength(2);
    expect(rows[0].text()).toContain("user_id");
    expect(rows[0].text()).toContain("integer");
    expect(rows[1].text()).toContain("user_name");
    expect(rows[1].text()).toContain("text");
  });

  it("filters the columns by name", async () => {
    const wrapper = mountDetail(table());

    await wrapper.find("input").setValue("user_name");

    const rows = wrapper.findAll("tbody tr");
    expect(rows).toHaveLength(1);
    expect(rows[0].text()).toContain("user_name");
  });
});
