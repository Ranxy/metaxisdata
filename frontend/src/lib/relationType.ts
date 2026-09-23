import { RelationType } from "@/types/proto-es/v1/lineage_service_pb";

// relationTypeKey maps a stored lineage relation type to the i18n key that labels
// it. The relation type says how a source column reaches its target — directly,
// through a transformation, as a join key, as an aggregate, or as one arm of a
// set operation — so collapsing every non-direct kind into one label would hide
// what the analyzers record.
export function relationTypeKey(
  relationType: RelationType | number
): string | undefined {
  switch (relationType) {
    case RelationType.DIRECT:
      return "metadataBrowser.relationDirect";
    case RelationType.INDIRECT:
      return "metadataBrowser.relationIndirect";
    case RelationType.JOIN:
      return "metadataBrowser.relationJoin";
    case RelationType.GROUP:
      return "metadataBrowser.relationGroup";
    case RelationType.UNION:
      return "metadataBrowser.relationUnion";
    case RelationType.INTERSECT:
      return "metadataBrowser.relationIntersect";
    case RelationType.EXCEPT:
      return "metadataBrowser.relationExcept";
    case RelationType.UNKNOWN:
      return "metadataBrowser.relationUnknown";
    default:
      return undefined;
  }
}
