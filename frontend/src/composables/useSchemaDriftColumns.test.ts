import { describe, expect, it } from "vitest";
import { ref } from "vue";
import type {
  RuleConfigForm,
  SchemaDriftColumnForm,
  TargetColumnDetail,
} from "@/utils/dataQualityForm";
import { useSchemaDriftColumns } from "./useSchemaDriftColumns";

function setup(
  expectedColumns: SchemaDriftColumnForm[] = [],
  columnDetails: TargetColumnDetail[] = []
) {
  const config = ref<RuleConfigForm>({ expectedColumns });
  const columns = ref(columnDetails);
  return {
    config,
    columns,
    ...useSchemaDriftColumns(config, columns),
  };
}

const target: TargetColumnDetail[] = [
  { name: "id", type: "bigint", nullable: false },
  { name: "email", type: "varchar", nullable: true },
];

describe("useSchemaDriftColumns", () => {
  it("adds a column with no judgement about nullability", () => {
    const { config, addColumn } = setup();

    addColumn();

    expect(config.value.expectedColumns).toEqual([{ name: "", type: "" }]);
  });

  it("removes a column by position", () => {
    const { config, removeColumn } = setup([
      { name: "id", type: "bigint" },
      { name: "email", type: "varchar" },
    ]);

    removeColumn(0);

    expect(config.value.expectedColumns).toEqual([
      { name: "email", type: "varchar" },
    ]);
  });

  it("edits only the name while typing", () => {
    const { config, setName } = setup([{ name: "id", type: "bigint" }]);
    const row = config.value.expectedColumns?.[0] as SchemaDriftColumnForm;

    setName(row, "identifier");

    expect(row).toEqual({ name: "identifier", type: "bigint" });
  });

  it("fills a matching target column's type and nullability on commit", () => {
    const { config, commitName } = setup([{ name: "id", type: "" }], target);
    const row = config.value.expectedColumns?.[0] as SchemaDriftColumnForm;

    commitName(row);

    expect(row).toEqual({ name: "id", type: "bigint", nullable: false });
  });

  it("clears what it filled when the name stops matching", () => {
    const { config, commitName } = setup([{ name: "id", type: "" }], target);
    const row = config.value.expectedColumns?.[0] as SchemaDriftColumnForm;
    commitName(row);

    row.name = "idx";
    commitName(row);

    expect(row).toEqual({ name: "idx", type: "", nullable: undefined });
  });

  it("does not overwrite a type the reader typed", () => {
    const { config, commitName, setType } = setup(
      [{ name: "id", type: "" }],
      target
    );
    const row = config.value.expectedColumns?.[0] as SchemaDriftColumnForm;

    setType(row, "uuid");
    commitName(row);

    expect(row.type).toBe("uuid");
    expect(row.nullable).toBe(false);
  });

  it("does not overwrite a nullability the reader toggled", () => {
    const { config, commitName, setNullable } = setup(
      [{ name: "id", type: "" }],
      target
    );
    const row = config.value.expectedColumns?.[0] as SchemaDriftColumnForm;

    setNullable(row, true);
    commitName(row);

    expect(row.nullable).toBe(true);
  });

  it("refreshes its own auto-filled values when the name changes to a match", () => {
    const { config, commitName } = setup([{ name: "id", type: "" }], target);
    const row = config.value.expectedColumns?.[0] as SchemaDriftColumnForm;
    commitName(row);

    row.name = "email";
    commitName(row);

    expect(row).toEqual({ name: "email", type: "varchar", nullable: true });
  });

  it("leaves a saved snapshot's values alone until the name matches", () => {
    const { config, commitName } = setup(
      [{ name: "custom", type: "text", nullable: true }],
      target
    );
    const row = config.value.expectedColumns?.[0] as SchemaDriftColumnForm;

    commitName(row);

    expect(row).toEqual({ name: "custom", type: "text", nullable: true });
  });

  it("replaces the snapshot with the target's columns", () => {
    const { config, fillFromTarget } = setup([], target);

    fillFromTarget();

    expect(config.value.expectedColumns).toEqual([
      { name: "id", type: "bigint", nullable: false },
      { name: "email", type: "varchar", nullable: true },
    ]);
  });

  it("treats a name match as one row and leaves the others untouched", () => {
    const { config, commitName } = setup(
      [
        { name: "id", type: "" },
        { name: "email", type: "" },
      ],
      target
    );
    const [id, email] = config.value.expectedColumns as SchemaDriftColumnForm[];

    commitName(id);

    expect(id.type).toBe("bigint");
    expect(email).toEqual({ name: "email", type: "" });
  });
});
