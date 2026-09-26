import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  ColumnMetadataSchema,
  ExternalTableMetadataSchema,
  MetaType,
  type StoredMetadata,
  StoredMetadataSchema,
} from "@/types/proto-es/v1/database_service_pb";
import MetadataList from "./MetadataList.vue";

function externalTable(name: string, server: string): StoredMetadata {
  return create(StoredMetadataSchema, {
    type: {
      case: "externalTableMetadata",
      value: create(ExternalTableMetadataSchema, {
        name,
        externalServerName: server,
        columns: [create(ColumnMetadataSchema, { name: "user_id" })],
      }),
    },
  });
}

// The schema tab hands every object type to this dispatcher; a type it does not
// know renders whatever the default is, which used to be the database list.
describe("MetadataList", () => {
  it("renders the external table list for the external table group", () => {
    const wrapper = mount(MetadataList, {
      props: {
        metaType: MetaType.EXTERNAL_TABLE,
        items: [externalTable("foreign_users", "remote_server")],
      },
      global: { plugins: [i18n] },
    });

    expect(wrapper.text()).toContain("foreign_users");
    expect(wrapper.text()).toContain("remote_server");
  });
});
