package dataquality

import (
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality/codetable"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The standard-code rule reads a bounded sample of one text column and
// validates every value in this process, exactly like a CEL expression: the
// standards are check-digit algorithms and offline code tables, neither of which
// a dialect can express. The sample bounds mirror cel.go's.
const (
	// defaultStandardCodeSampleRows is the sample size when the config leaves it
	// unset. The sample is the execution budget.
	defaultStandardCodeSampleRows = 10000
	// maxStandardCodeSampleRows bounds one sample query.
	maxStandardCodeSampleRows = 100000
)

// compileStandardCode builds the sample-row statement a standard-code rule reads.
// The values themselves are validated in Go, so the statement selects one column
// and is bounded exactly like a CEL sample.
func compileStandardCode(input CompileInput) (CompiledQuery, error) {
	config := input.Config.GetStandardCode()
	if config == nil {
		return CompiledQuery{}, invalidConfig("the standard-code config is missing")
	}
	column, err := requireTextColumn(input.Target, config.GetColumn())
	if err != nil {
		return CompiledQuery{}, err
	}
	sampleRows, err := standardCodeSampleRows(config.GetSampleRows())
	if err != nil {
		return CompiledQuery{}, err
	}
	table, err := compileTable(input.Dialect, input.Target)
	if err != nil {
		return CompiledQuery{}, err
	}
	where, filterArgs, err := whereClause(input)
	if err != nil {
		return CompiledQuery{}, err
	}
	args := newSampleArgs(input.Dialect, filterArgs)
	// The sample carries the target's key columns as well as the checked value, so
	// a failing row is identifiable: the checked value alone cannot say which row
	// broke the standard.
	names := sampleColumns(input.Target, column.Name)
	return sampleQuery(input,
		"SELECT "+sampleSelect(input.Dialect, names)+" FROM "+table+where,
		args.values, names, sampleRows), nil
}

// standardCodeSampleRows maps the configured sample size to the number of rows
// the sample query asks for: unset reads as the default, and the maximum is
// refused rather than silently clamped so the operator sees the limit they
// crossed.
func standardCodeSampleRows(configured int32) (int, error) {
	if configured < 0 {
		return 0, invalidConfig("sample_rows must not be negative")
	}
	if configured == 0 {
		return defaultStandardCodeSampleRows, nil
	}
	if int(configured) > maxStandardCodeSampleRows {
		return 0, invalidConfig("sample_rows %d is above the maximum of %d", configured, maxStandardCodeSampleRows)
	}
	return int(configured), nil
}

// validateStandardCodeConfig checks the standard-code rule: a text column, a
// known standard, a usable sample size and count thresholds. The value rules
// themselves are pure functions and cannot be invalid.
func validateStandardCodeConfig(target ResolvedTarget, config *storepb.QualityRuleConfig) error {
	standard := config.GetStandardCode()
	if standard == nil {
		return invalidConfig("the standard-code config is missing")
	}
	if _, err := requireTextColumn(target, standard.GetColumn()); err != nil {
		return err
	}
	switch standard.GetStandard() {
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNSPECIFIED:
		return invalidConfig("a standard-code rule needs a standard")
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE:
		// The reference is resolved and canonicalized by the API; validation only
		// proves one was supplied, because the component has no store to look it up.
		if strings.TrimSpace(standard.GetCodeTable()) == "" {
			return invalidConfig("a custom code table standard requires a code_table reference")
		}
	default:
		if !knownStandard(standard.GetStandard()) {
			return invalidConfig("standard %s is not supported by this build", standard.GetStandard())
		}
	}
	if _, err := standardCodeSampleRows(standard.GetSampleRows()); err != nil {
		return err
	}
	return validateCountThresholds(config, standard.FailThreshold, standard.WarnThreshold)
}

// standardCodeValueIndex returns the position of the checked column in the
// compiled sample's column list.
func standardCodeValueIndex(columns []string, column string) (int, error) {
	for index, name := range columns {
		if name == column {
			return index, nil
		}
	}
	return 0, badData("the compiled sample does not select the checked column %q", column)
}

// evaluateStandardCodeRows validates the checked value of every sampled row. It
// records how many rows it read and, up to failingLimit, the offending rows
// themselves (including their key columns). codeTable is nil when the rule names
// no workspace code table; when it is set, a value must also be a member.
func evaluateStandardCodeRows(standard storepb.QualityStandardCode, allowNull bool, valueIndex int, rows [][]any, sampleLimit int, codeTable map[string]struct{}) (violations, evaluated int64, failing, passing [][]any, err error) {
	for rowIndex, row := range rows {
		if valueIndex < 0 || valueIndex >= len(row) {
			return 0, 0, nil, nil, badData("sample row %d has %d values, expect the checked value at %d", rowIndex, len(row), valueIndex)
		}
		value, isNull, err := standardCodeCell(row[valueIndex])
		if err != nil {
			return 0, 0, nil, nil, err
		}
		evaluated++
		if (isNull && allowNull) || (!isNull && standardCodeAccepts(standard, value, codeTable)) {
			if sampleLimit > 0 && len(passing) < sampleLimit {
				passing = append(passing, row)
			}
			continue
		}
		violations++
		if sampleLimit > 0 && len(failing) < sampleLimit {
			failing = append(failing, row)
		}
	}
	return violations, evaluated, failing, passing, nil
}

// standardCodeCell reads one selected cell of a sample row.
func standardCodeCell(value any) (string, bool, error) {
	switch typed := value.(type) {
	case nil:
		return "", true, nil
	case string:
		return typed, false, nil
	case []byte:
		return string(typed), false, nil
	default:
		return "", false, badData("a standard-code rule read a %T value, expect text", value)
	}
}

// knownStandard reports whether standard names one of the standards this build
// validates. An unknown non-zero value is a stored-data defect, not a rule that
// fails every value.
func knownStandard(standard storepb.QualityStandardCode) bool {
	switch standard {
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
		storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE,
		storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD,
		storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION,
		storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION,
		storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE:
		return true
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNSPECIFIED:
		return false
	default:
		return false
	}
}

// standardCodeTableSet builds the accepted-value set of one rule's code table. A
// rule without a reference has no set, and a built-in standard stays valid on its
// own. A reference with no loaded values is a defect rather than an empty
// allow-list: for a custom code table it means the table is empty or was not
// loaded, and for a built-in standard the narrowing filter would silently reject
// every value.
func standardCodeTableSet(standard storepb.QualityStandardCode, reference string, values []string) (map[string]struct{}, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		if standard == storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE {
			return nil, invalidConfig("a custom code table standard requires a code_table reference")
		}
		return nil, nil
	}
	if len(values) == 0 {
		return nil, invalidConfig("the code table %q has no values; add codes or clear the rule's code table", reference)
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		// A built-in standard normalizes case in its own validator, so its narrowing
		// set must be normalized the same way; a workspace custom table is
		// case-sensitive and its codes are authoritative.
		if standard != storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE {
			value = strings.ToUpper(strings.TrimRight(value, " "))
		}
		set[value] = struct{}{}
	}
	return set, nil
}

// standardCodeAccepts reports whether one sampled value satisfies the rule. A
// custom code table has no built-in algorithm, so membership alone decides. A
// built-in standard always checks its own algorithm and, when the rule narrows it
// with a code table, must also be a member of that table.
func standardCodeAccepts(standard storepb.QualityStandardCode, value string, codeTable map[string]struct{}) bool {
	if standard != storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE && !validStandardCode(standard, value) {
		return false
	}
	if codeTable == nil {
		return true
	}
	return standardCodeTableContains(standard, codeTable, value)
}

// standardCodeTableContains looks one value up in the accepted-value set. The code
// table's stored values are trimmed; the sampled value keeps its leading
// whitespace (padding on a fixed-width CHAR is tolerated, a leading space is not). A
// built-in standard normalizes case the way its validator does, while a custom
// table is case-sensitive: the workspace's codes are authoritative.
func standardCodeTableContains(standard storepb.QualityStandardCode, codeTable map[string]struct{}, value string) bool {
	value = strings.TrimRight(value, " ")
	if standard != storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE {
		value = strings.ToUpper(value)
	}
	_, ok := codeTable[value]
	return ok
}

// validStandardCode dispatches one value to its standard's pure validator.
func validStandardCode(standard storepb.QualityStandardCode, value string) bool {
	switch standard {
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE:
		return validUnifiedSocialCreditCode(value)
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ORGANIZATION_CODE:
		return validOrganizationCode(value)
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_RESIDENT_ID_CARD:
		return validResidentIDCard(value)
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_ADMINISTRATIVE_DIVISION:
		return validAdministrativeDivision(value)
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_INDUSTRY_CLASSIFICATION:
		return validIndustryClassification(value)
	case storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNSPECIFIED:
		return false
	default:
		// Validation and the executor reject an unknown standard before any value
		// is judged; treating every value as a violation would be a silent wrong
		// verdict if a stored rule ever reached here.
		return false
	}
}

// maskedResidentIDCardValue keeps the six-digit division prefix and the last four
// characters visible (the check character among them) and replaces everything
// between them. A value too short to keep both ends visible is fully redacted.
func maskedResidentIDCardValue(value string) string {
	characters := []rune(value)
	if len(characters) < 10 {
		return strings.Repeat("*", len(characters))
	}
	return string(characters[:6]) + strings.Repeat("*", len(characters)-10) + string(characters[len(characters)-4:])
}

// usccAlphabet is the GB 32100-2015 character set: digits followed by the
// uppercase Latin letters the standard allows (I, O, S, V and Z are excluded).
const usccAlphabet = "0123456789ABCDEFGHJKLMNPQRTUWXY"

// unifiedSocialCreditWeights are 3^(i-1) mod 31 for the first 17 characters.
var unifiedSocialCreditWeights = [17]int{1, 3, 9, 27, 19, 26, 16, 17, 20, 29, 25, 13, 8, 24, 10, 30, 28}

// validUnifiedSocialCreditCode validates a GB 32100-2015 code: the registrar
// prefix, the six-digit administrative code, the 31-character set and the MOD 31
// check character.
func validUnifiedSocialCreditCode(value string) bool {
	code := strings.ToUpper(strings.TrimRight(value, " "))
	if len(code) != 18 {
		return false
	}
	if !strings.ContainsRune("123456789ANY", rune(code[0])) {
		return false
	}
	if !strings.ContainsRune("123459", rune(code[1])) {
		return false
	}
	for index := 2; index < 8; index++ {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}
	sum := 0
	for index := 0; index < 17; index++ {
		digit := strings.IndexByte(usccAlphabet, code[index])
		if digit < 0 {
			return false
		}
		sum += digit * unifiedSocialCreditWeights[index]
	}
	return code[17] == usccAlphabet[(31-sum%31)%31]
}

// organizationCodeWeights are the GB/T 11714-1997 positional weights.
var organizationCodeWeights = [8]int{3, 7, 9, 10, 5, 8, 4, 2}

// validOrganizationCode validates a GB/T 11714-1997 code: eight body characters
// from 0-9/A-Z plus a MOD 11 check character, with an optional hyphen before the
// check character.
func validOrganizationCode(value string) bool {
	code := strings.ToUpper(strings.TrimRight(value, " "))
	code = strings.ReplaceAll(code, "－", "-")
	if len(code) == 10 {
		if code[8] != '-' {
			return false
		}
		code = code[:8] + code[9:]
	}
	if len(code) != 9 {
		return false
	}
	sum := 0
	for index := 0; index < 8; index++ {
		character := organizationCodeValue(code[index])
		if character < 0 {
			return false
		}
		sum += character * organizationCodeWeights[index]
	}
	check := 11 - sum%11
	var want byte
	switch check {
	case 10:
		want = 'X'
	case 11:
		want = '0'
	default:
		want = byte('0' + check)
	}
	return code[8] == want
}

// organizationCodeValue maps one body character to its GB/T 11714 value: digits
// are themselves, letters A-Z are 10-35.
func organizationCodeValue(character byte) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'A' && character <= 'Z':
		return int(character-'A') + 10
	default:
		return -1
	}
}

// residentIDCardWeights are 2^(18-i) mod 11 for the first 17 digits.
var residentIDCardWeights = [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}

// residentIDCardCheckCharacters maps sum mod 11 to the check character.
const residentIDCardCheckCharacters = "10X98765432"

// validResidentIDCard validates a GB 11643-1999 citizen number: 17 digits, a
// real birth date, a province-level division prefix and the MOD 11 check
// character. The full six-digit division table is deliberately not consulted:
// an address code belongs to the region at issue time, and retired codes are
// legitimate history rather than defects.
func validResidentIDCard(value string) bool {
	code := strings.ToUpper(strings.TrimRight(value, " "))
	if len(code) != 18 {
		return false
	}
	for index := 0; index < 17; index++ {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}
	last := code[17]
	if (last < '0' || last > '9') && last != 'X' {
		return false
	}
	birth := code[6:14]
	if _, err := time.Parse("20060102", birth); err != nil || birth < "18000101" || birth > time.Now().UTC().Format("20060102") {
		return false
	}
	if !codetable.IsProvincePrefix(code[0:2]) {
		return false
	}
	sum := 0
	for index := 0; index < 17; index++ {
		sum += int(code[index]-'0') * residentIDCardWeights[index]
	}
	return code[17] == residentIDCardCheckCharacters[sum%11]
}

// validAdministrativeDivision validates a GB/T 2260 six-digit code against the
// embedded division table.
func validAdministrativeDivision(value string) bool {
	code := strings.TrimRight(value, " ")
	if len(code) != 6 {
		return false
	}
	for index := 0; index < 6; index++ {
		if code[index] < '0' || code[index] > '9' {
			return false
		}
	}
	return codetable.IsAdministrativeDivision(code)
}

// validIndustryClassification validates a GB/T 4754-2017 code against the
// embedded classification table. An optional leading section letter must agree
// with the section that owns the numeric code.
func validIndustryClassification(value string) bool {
	return codetable.IsIndustryClassification(strings.ToUpper(strings.TrimRight(value, " ")))
}
