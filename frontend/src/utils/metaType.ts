import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { Translate } from "./i18n";

/**
 * The display name of a metadata type, shared by the type tabs, the filters and
 * the badges that used to carry their own English table.
 *
 * The lookup calls below are deliberately literals rather than a key table:
 * both `scripts/check-vue-i18n.mjs` and ESLint's `no-unused-keys` follow lookup
 * calls, but neither can follow a key held in a map value.
 */
export function metaTypeLabel(metaType: MetaType, t: Translate): string {
  switch (metaType) {
    case MetaType.INSTANCE:
      return t("metadataBrowser.instances");
    case MetaType.DATABASE:
      return t("metadataBrowser.databases");
    case MetaType.SCHEMA:
      return t("metadataBrowser.schemas");
    case MetaType.TABLE:
      return t("metadataBrowser.tables");
    case MetaType.COLUMN:
      return t("metadataBrowser.columns");
    case MetaType.INDEX:
      return t("metadataBrowser.indexes");
    case MetaType.FOREIGN_KEY:
      return t("metadataBrowser.foreignKeys");
    case MetaType.EXTERNAL_TABLE:
      return t("metadataBrowser.externalTables");
    case MetaType.EXTERNAL_DATASET:
      return t("metadataBrowser.externalDatasets");
    case MetaType.VIEW:
      return t("metadataBrowser.views");
    case MetaType.MATERIALIZED_VIEW:
      return t("metadataBrowser.materializedViews");
    case MetaType.FUNCTION:
      return t("metadataBrowser.functions");
    case MetaType.PROCEDURE:
      return t("metadataBrowser.procedures");
    case MetaType.SEQUENCE:
      return t("metadataBrowser.sequences");
    case MetaType.MANUAL_SQL:
      return t("metadataBrowser.manualSqls");
    case MetaType.OPENLINEAGE:
      return t("openlineage.title");
    default:
      return t("metadataBrowser.other");
  }
}
