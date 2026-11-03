package dataquality

import (
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// A column whose accepted semantic label is personal data must not have its raw
// values persisted as evidence. The product stores two kinds of evidence: the
// failed-row samples of an execution and the cached column profile. Both are
// redacted for these labels; the value itself is still validated, only the stored
// display form is replaced.

// IsPersonalSemanticType reports whether a semantic label names personal data.
// The set is deliberately small: a resident ID card number identifies a person,
// while business identifiers (social credit code, organization code) identify an
// organization.
func IsPersonalSemanticType(label storepb.QualitySemanticType) bool {
	switch label {
	case storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_RESIDENT_ID_CARD:
		return true
	case storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNSPECIFIED,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_EMAIL,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_PHONE,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_URL,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UUID,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_IP,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_COUNTRY_CODE,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_CURRENCY,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_AMOUNT,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_QUANTITY,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_PERCENTAGE,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ID,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_BUSINESS_KEY,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_TIMESTAMP,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_DATE,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ENUM,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_FREE_TEXT,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNKNOWN,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_UNIFIED_SOCIAL_CREDIT_CODE,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ORGANIZATION_CODE,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_ADMINISTRATIVE_DIVISION,
		storepb.QualitySemanticType_QUALITY_SEMANTIC_TYPE_INDUSTRY_CLASSIFICATION:
		return false
	default:
		return false
	}
}

// MaskPersonalValue returns the display form of one personal value: a resident ID
// card number keeps its division prefix and last four characters and everything
// between is replaced. A value too short to keep both ends is fully redacted.
func MaskPersonalValue(value string) string {
	return maskedResidentIDCardValue(value)
}

// RedactPersonalProfile replaces a personal column's profile values with their
// redacted display forms. Counts, lengths and shape summaries are statistics about
// the column, not its values, so they stay; the observed extremes and frequent
// values do not.
func RedactPersonalProfile(profile *storepb.ColumnProfile) {
	if profile == nil {
		return
	}
	profile.MinValue = MaskPersonalValue(profile.GetMinValue())
	profile.MaxValue = MaskPersonalValue(profile.GetMaxValue())
	profile.TopValues = nil
}

// redactPersonalSamples replaces the cells of the named columns with their masked
// display forms and marks them. NULL cells keep their null marker; only a stored
// value is redacted.
func redactPersonalSamples(samples *storepb.QualityExecutionEvidence, columns map[string]struct{}) {
	if samples == nil || len(columns) == 0 {
		return
	}
	for _, sample := range samples.GetSamples() {
		for _, cell := range sample.GetColumns() {
			if cell == nil || cell.GetIsNull() {
				continue
			}
			if _, ok := columns[cell.GetColumn()]; !ok {
				continue
			}
			cell.Value = MaskPersonalValue(cell.GetValue())
			cell.Masked = proto.Bool(true)
		}
	}
}
