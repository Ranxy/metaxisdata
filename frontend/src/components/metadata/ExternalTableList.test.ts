import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  ColumnMetadataSchema,
  ExternalTableMetadataSchema,
  type StoredMetadata,
  StoredMetadataSchema,
  TableMetadataSchema,
} from "@/types/proto-es/v1/database_service_pb";
import ExternalTableList from "./ExternalTableList.vue";

function externalTable(
  name: string,
  server: string,
  database: string,
  columns: string[]
): StoredMetadata {
  return create(StoredMetadataSchema, {
    type: {
      case: "externalTableMetadata",
      value: create(ExternalTableMetadataSchema, {
        name,
        externalServerName: server,
        externalDatabaseName: database,
        columns: columns.map((column) =>
          create(ColumnMetadataSchema, { name: column })
        ),
      }),
    },
  });
}

function mountList(items: StoredMetadata[]) {
  return mount(ExternalTableList, {
    props: { items },
    global: { plugins: [i18n] },
  });
}

describe("ExternalTableList", () => {
  it("lists only the external tables with their server, database and columns", () => {
    const foreign = externalTable(
      "foreign_users",
      "remote_server",
      "remote_db",
      ["user_id", "user_name"]
    );
    const wrapper = mountList([
      foreign,
      create(StoredMetadataSchema, {
        type: {
          case: "tableMetadata",
          value: create(TableMetadataSchema, { name: "users" }),
        },
      }),
    ]);

    const rows = wrapper.findAll("tbody tr");
    expect(rows).toHaveLength(1);
    expect(rows[0].text()).toContain("foreign_users");
    expect(rows[0].text()).toContain("remote_server");
    expect(rows[0].text()).toContain("remote_db");
    expect(rows[0].text()).toContain("2 columns");
  });

  it("emits the stored metadata of the row that was selected", async () => {
    const foreign = externalTable(
      "foreign_users",
      "remote_server",
      "remote_db",
      []
    );
    const wrapper = mountList([foreign]);

    await wrapper.find("tbody tr").trigger("click");

    expect(wrapper.emitted("select")?.[0]).toEqual([foreign]);
  });

  it("reports an empty list rather than rendering an empty table", () => {
    const wrapper = mountList([
      create(StoredMetadataSchema, {
        type: {
          case: "tableMetadata",
          value: create(TableMetadataSchema, { name: "users" }),
        },
      }),
    ]);

    expect(wrapper.findAll("tbody tr")).toHaveLength(0);
    expect(wrapper.text()).toContain("No external tables");
  });
});
