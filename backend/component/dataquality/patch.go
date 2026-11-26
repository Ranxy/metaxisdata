package dataquality

import (
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// PatchField returns a copy of a check's config with one named field set to value.
//
// The set of fields is exactly the set a bound-value diagnosis can name (see the configField
// constants): a proposal always asks for a change to the field the server itself pointed at, and
// a field that can be reported but not written would be a dead end for the model and the owner
// alike. Only string-valued fields are patchable, because those are the fields a diagnosis names.
//
// The config is copied, never mutated. That is what lets a proposal - a change nobody may apply -
// be validated with the same rule validation a save runs before it is recorded.
func PatchField(config *storepb.QualityRuleConfig, field, value string) (*storepb.QualityRuleConfig, error) {
	if config == nil {
		return nil, errors.New("the check has no config to change")
	}
	patched, ok := proto.Clone(config).(*storepb.QualityRuleConfig)
	if !ok {
		return nil, errors.New("the check's config could not be copied")
	}
	field = strings.TrimSpace(field)
	switch field {
	case configFieldRangeMin:
		container, ok := patched.GetConfig().(*storepb.QualityRuleConfig_Range)
		if !ok {
			return nil, errors.Errorf("the check has no %s field", field)
		}
		container.Range.Min = &value
		return patched, nil
	case configFieldRangeMax:
		container, ok := patched.GetConfig().(*storepb.QualityRuleConfig_Range)
		if !ok {
			return nil, errors.Errorf("the check has no %s field", field)
		}
		container.Range.Max = &value
		return patched, nil
	case configFieldRegex:
		container, ok := patched.GetConfig().(*storepb.QualityRuleConfig_Regex)
		if !ok {
			return nil, errors.Errorf("the check has no %s field", field)
		}
		container.Regex.Pattern = value
		return patched, nil
	case configFieldExpression:
		container, ok := patched.GetConfig().(*storepb.QualityRuleConfig_CelExpression)
		if !ok {
			return nil, errors.Errorf("the check has no %s field", field)
		}
		container.CelExpression.Expression = value
		return patched, nil
	}
	path, index, ok := indexedConfigField(field)
	if !ok {
		return nil, errors.Errorf("%q is not a config field this build can set", field)
	}
	switch path {
	case configFieldNullValues:
		holder := patched.GetNullPercent()
		if holder == nil {
			return nil, errors.Errorf("the check has no %s field", field)
		}
		return patched, replaceElement(field, holder.NullValues, index, value)
	case configFieldAcceptedValues:
		holder := patched.GetAcceptedValues()
		if holder == nil {
			return nil, errors.Errorf("the check has no %s field", field)
		}
		return patched, replaceElement(field, holder.Values, index, value)
	}
	// A filter value's path carries two indices ("filter.clauses[1].values[0]"), so it is the one
	// field whose container is itself addressed by an index.
	if clauseField, found := strings.CutSuffix(path, ".values"); found {
		clausePath, clauseIndex, nested := indexedConfigField(clauseField)
		if !nested || clausePath != configFieldFilterValues {
			return nil, errors.Errorf("%q is not a config field this build can set", field)
		}
		clauses := patched.GetFilter().GetClauses()
		if clauseIndex < 0 || clauseIndex >= len(clauses) {
			return nil, errors.Errorf("%q names no filter clause", field)
		}
		return patched, replaceElement(field, clauses[clauseIndex].GetValues(), index, value)
	}
	return nil, errors.Errorf("%q is not a config field this build can set", field)
}

// replaceElement rewrites one element of a list-valued config field in place. Every caller works
// on a clone, so the caller's config is untouched.
func replaceElement(field string, values []string, index int, value string) error {
	if index < 0 || index >= len(values) {
		return errors.Errorf("%q names no value", field)
	}
	values[index] = value
	return nil
}

// indexedConfigField splits "accepted_values.values[2]" into its path and index. The filter
// values path nests two of them ("filter.clauses[1].values[0]"), so the caller splits from the
// right: every element of the path is addressed the same way.
func indexedConfigField(field string) (path string, index int, ok bool) {
	open := strings.LastIndex(field, "[")
	if open < 0 || !strings.HasSuffix(field, "]") {
		return "", 0, false
	}
	parsed, err := strconv.Atoi(field[open+1 : len(field)-1])
	if err != nil {
		return "", 0, false
	}
	return field[:open], parsed, true
}
