import type { Ref } from "vue";
import type {
  RuleConfigForm,
  SchemaDriftColumnForm,
  TargetColumnDetail,
} from "@/utils/dataQualityForm";

/** Which of a manual snapshot row's values this form filled from the target. */
interface ExpectedColumnAutoFill {
  type: boolean;
  nullable: boolean;
}

const EMPTY_COLUMN = (): SchemaDriftColumnForm => ({ name: "", type: "" });

/**
 * The schema-drift snapshot's expected-column rows.
 *
 * A row's values can come from three owners: the saved rule, the target (filled
 * automatically when a name matches), and the reader. Only values this
 * composable filled may be refreshed or cleared when the name changes; a value
 * the reader typed or toggled is theirs for the rest of the edit. That
 * ownership was a `WeakMap` inside the form component, with no test at all.
 */
export function useSchemaDriftColumns(
  config: Ref<RuleConfigForm>,
  columnDetails: Ref<TargetColumnDetail[]>
) {
  const autoFilled = new WeakMap<
    SchemaDriftColumnForm,
    ExpectedColumnAutoFill
  >();

  function autoFillMark(column: SchemaDriftColumnForm): ExpectedColumnAutoFill {
    let mark = autoFilled.get(column);
    if (!mark) {
      mark = { type: false, nullable: false };
      autoFilled.set(column, mark);
    }
    return mark;
  }

  function addColumn() {
    config.value.expectedColumns = [
      ...(config.value.expectedColumns ?? []),
      // Nullability starts unset: the rule should not judge it until a target
      // column or the reader supplies the expected bit.
      EMPTY_COLUMN(),
    ];
  }

  function removeColumn(index: number) {
    config.value.expectedColumns = (config.value.expectedColumns ?? []).filter(
      (_, position) => position !== index
    );
  }

  /** Typing only edits the name; the target lookup happens on blur. */
  function setName(column: SchemaDriftColumnForm, value: string) {
    column.name = value;
  }

  /**
   * Resolves a manual row against the target when the name field is committed
   * (blur). A matching column fills a blank or previously auto-filled type, and
   * a nullability the reader has not set; a name that matches nothing clears
   * what this behavior filled, so a transient match (`id` -> `idx`) cannot
   * leave stale expectations behind. Values typed by the reader, toggled by the
   * reader, or filled from the saved rule/from target carry no mark and are
   * never cleared or overwritten.
   */
  function commitName(column: SchemaDriftColumnForm) {
    const mark = autoFillMark(column);
    const match = columnDetails.value.find(
      (detail) => detail.name === column.name.trim()
    );
    if (!match) {
      if (mark.type) {
        column.type = "";
        mark.type = false;
      }
      if (mark.nullable) {
        column.nullable = undefined;
        mark.nullable = false;
      }
      return;
    }
    if (column.type.trim() === "" || mark.type) {
      column.type = match.type;
      mark.type = true;
    }
    if (column.nullable === undefined || mark.nullable) {
      column.nullable = match.nullable;
      mark.nullable = true;
    }
  }

  /** A type the reader edits is theirs: a later name match must not overwrite it. */
  function setType(column: SchemaDriftColumnForm, value: string) {
    column.type = value;
    autoFillMark(column).type = false;
  }

  /** Writes the snapshot bit and records that the reader chose it. */
  function setNullable(column: SchemaDriftColumnForm, value: boolean) {
    column.nullable = value;
    autoFillMark(column).nullable = false;
  }

  /** Replace the snapshot with the target's current columns. */
  function fillFromTarget() {
    config.value.expectedColumns = columnDetails.value.map(
      (column): SchemaDriftColumnForm => ({
        name: column.name,
        type: column.type,
        nullable: column.nullable,
      })
    );
  }

  return {
    addColumn,
    removeColumn,
    setName,
    commitName,
    setType,
    setNullable,
    fillFromTarget,
  };
}
