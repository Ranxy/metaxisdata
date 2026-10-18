/**
 * `datasetType` is a free string from the OpenLineage producer, but the values
 * this build stores are English identifiers (`database`, `unknown`). Both the
 * datasets table and the detail drawer render it, so the rule lives here rather
 * than in either component. An unrecognized value stays raw: inventing a label
 * would hide which type the producer actually sent.
 */
export function openLineageDatasetTypeLabel(
  datasetType: string | undefined,
  t: (key: string) => string
): string {
  switch ((datasetType ?? "").toLowerCase()) {
    case "database":
      return t("openlineage.datasetTypeDatabase");
    case "":
    case "unknown":
      return t("openlineage.datasetTypeUnknown");
    default:
      return datasetType ?? "-";
  }
}
