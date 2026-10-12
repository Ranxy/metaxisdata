import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import SchemaDefinitionDialog from "./SchemaDefinitionDialog.vue";

const mocks = vi.hoisted(() => ({ getSchemaString: vi.fn() }));

vi.mock("@/api/database", () => ({ getSchemaString: mocks.getSchemaString }));

/**
 * An empty definition keeps the Monaco viewer out of the test — the component renders its
 * plain-text fallback — and what these cases are about is the header above it. The dialog
 * teleports its content, so the assertions read the document.
 */
async function mountDialog(description?: string) {
  mocks.getSchemaString.mockResolvedValue({ schema: "" });
  const wrapper = mount(SchemaDefinitionDialog, {
    props: {
      guid: "inst1;db1;public;t1",
      metaType: MetaType.TABLE,
      objectName: "t1",
      open: true,
      ...(description === undefined ? {} : { description }),
    },
    global: { plugins: [i18n] },
    attachTo: document.body,
  });
  await flushPromises();
  return wrapper;
}

describe("SchemaDefinitionDialog", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    mocks.getSchemaString.mockReset();
  });

  it("describes a schema-driven object as its DDL by default", async () => {
    const wrapper = await mountDialog();

    expect(document.body.textContent).toContain("Schema Definition (DDL)");
    wrapper.unmount();
  });

  // A reported statement is not a definition reconstructed from a schema, and claiming it
  // is would be the one wrong thing the shared dialog could tell a reader.
  it("takes the caller's description when one is given", async () => {
    const wrapper = await mountDialog("Literals replaced with ?.");

    expect(document.body.textContent).toContain("Literals replaced with ?.");
    expect(document.body.textContent).not.toContain("Schema Definition (DDL)");
    wrapper.unmount();
  });
});
