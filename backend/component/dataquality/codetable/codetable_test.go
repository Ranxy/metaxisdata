package codetable

import (
	"strings"
	"testing"
)

func TestIsAdministrativeDivision(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{"province aggregate", "110000", true},
		{"city aggregate", "110100", true},
		{"county", "110101", true},
		{"municipality", "310000", true},
		{"province", "440000", true},
		{"city", "440300", true},
		{"district", "440305", true},
		{"municipality aggregate", "500000", true},
		{"province-directly-administered aggregate", "419000", true},
		{"province-directly-administered city", "419001", true},
		{"province-directly-administered county", "659001", true},
		{"empty", "", false},
		{"five digits", "11000", false},
		{"seven digits", "1100000", false},
		{"unknown code", "999999", false},
		{"letters", "abcdef", false},
		{"trailing letter", "11000a", false},
		{"statistics development zone", "320571", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAdministrativeDivision(tt.code); got != tt.want {
				t.Errorf("IsAdministrativeDivision(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestIsProvincePrefix(t *testing.T) {
	for _, prefix := range provincePrefixList {
		if !IsProvincePrefix(prefix) {
			t.Errorf("IsProvincePrefix(%q) = false, want true", prefix)
		}
	}
	for _, code := range []string{"00", "16", "99", "9", "", "110"} {
		if IsProvincePrefix(code) {
			t.Errorf("IsProvincePrefix(%q) = true, want false", code)
		}
	}
}

func TestIsIndustryClassification(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{"section only", "A", true},
		{"lowercase section only", "a", true},
		{"section with division", "A01", true},
		{"section with group", "A011", true},
		{"section with class", "A0111", true},
		{"manufacturing division", "C39", true},
		{"manufacturing class", "C3911", true},
		{"information division", "I65", true},
		{"information class", "I6511", true},
		{"numeric division", "39", true},
		{"numeric class", "3911", true},
		{"lowercase section and code", "c3911", true},
		{"amended public-security class", "S9233", true},
		{"bare numeric division is valid", "65", true},
		{"missing numeric code", "I6510", false},
		{"wrong section for division", "A39", false},
		{"wrong section for group", "C01", false},
		{"wrong section for class", "A3911", false},
		{"wrong section reversed", "I39", false},
		{"unknown numeric code", "A99", false},
		{"unknown two-digit code", "C00", false},
		{"unknown four-digit code", "A0199", false},
		{"unknown section letter", "Z01", false},
		{"section plus one digit", "A0", false},
		{"section plus five digits", "A01111", false},
		{"empty", "", false},
		{"separator", "A-01", false},
		{"numeric with letter suffix", "01A", false},
		{"two letters", "AA01", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsIndustryClassification(tt.code); got != tt.want {
				t.Errorf("IsIndustryClassification(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestDivisionDataSanity(t *testing.T) {
	if got := len(divisionCodes); got < 3200 {
		t.Fatalf("division code count = %d, want >= 3200", got)
	}
	if got := len(provincePrefixes); got != len(provincePrefixList) {
		t.Fatalf("province prefix count = %d, want %d", got, len(provincePrefixList))
	}

	allowed := make(map[string]struct{}, len(provincePrefixList))
	for _, prefix := range provincePrefixList {
		allowed[prefix] = struct{}{}
	}

	seen := make(map[string]struct{}, len(divisionCodes))
	accepted := 0
	for n, line := range strings.Split(divisionData, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) != 6 {
			t.Errorf("line %d: %q is not 6 characters", n+1, line)
			continue
		}
		for i := 0; i < len(line); i++ {
			if line[i] < '0' || line[i] > '9' {
				t.Errorf("line %d: %q is not all digits", n+1, line)
			}
		}
		if _, ok := seen[line]; ok {
			t.Errorf("line %d: duplicate division code %q", n+1, line)
		}
		seen[line] = struct{}{}
		if isStatisticsPseudoDivision(line) {
			continue
		}
		accepted++
		if _, ok := allowed[line[:2]]; !ok {
			t.Errorf("line %d: %q has province prefix %q outside the 34", n+1, line, line[:2])
		}
	}
	if accepted != len(divisionCodes) {
		t.Errorf("accepted data lines = %d, loaded codes = %d", accepted, len(divisionCodes))
	}

	// The source is a statistics-bureau dataset whose development-zone pseudo
	// divisions were removed during generation. Re-assert the contract here so a
	// future data refresh cannot silently reintroduce them: GB/T 2260 has no
	// county codes ending 71-79, and the known statistics-only codes are absent.
	for _, code := range []string{"130171", "131071", "320571", "320576", "340176", "411571"} {
		if _, ok := divisionCodes[code]; ok {
			t.Errorf("loaded table contains statistics-bureau pseudo division %q", code)
		}
	}
	for code := range divisionCodes {
		if last := code[4:]; last >= "71" && last <= "79" {
			t.Errorf("loaded table contains %q, whose county segment ends 71-79", code)
		}
	}
}

func TestIndustryDataSanity(t *testing.T) {
	if got := len(industrySections); got < 900 {
		t.Fatalf("industry numeric code count = %d, want >= 900", got)
	}

	wantSections := make(map[byte]struct{}, len("ABCDEFGHIJKLMNOPQRST"))
	for c := byte('A'); c <= 'T'; c++ {
		wantSections[c] = struct{}{}
	}

	seen := make(map[string]struct{}, len(industrySections))
	sectionCodes := make(map[byte]int)
	for n, line := range strings.Split(industryData, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Errorf("line %d: want two fields, got %q", n+1, line)
			continue
		}
		letter := fields[0]
		number := fields[1]
		if len(letter) != 1 || letter[0] < 'A' || letter[0] > 'T' {
			t.Errorf("line %d: invalid section %q", n+1, letter)
			continue
		}
		if len(number) < 2 || len(number) > 4 {
			t.Errorf("line %d: %q is not a 2, 3 or 4 digit code", n+1, number)
			continue
		}
		for i := 0; i < len(number); i++ {
			if number[i] < '0' || number[i] > '9' {
				t.Errorf("line %d: %q is not all digits", n+1, number)
			}
		}
		if _, ok := seen[number]; ok {
			t.Errorf("line %d: duplicate industry code %q", n+1, number)
		}
		seen[number] = struct{}{}
		sectionCodes[letter[0]]++
		if got, ok := industrySections[number]; !ok || got != letter {
			t.Errorf("line %d: loaded ownership of %q = %q, %v; want %q", n+1, number, got, ok, letter)
		}
	}

	if len(sectionCodes) != len(wantSections) {
		t.Errorf("section count = %d, want %d", len(sectionCodes), len(wantSections))
	}
	for letter := range wantSections {
		if sectionCodes[letter] == 0 {
			t.Errorf("section %c has no codes", letter)
		}
	}

	// Every 中类/小类 must have its parent in the same section.
	for code, section := range industrySections {
		var parent string
		switch len(code) {
		case 3:
			parent = code[:2]
		case 4:
			parent = code[:3]
		default:
			continue
		}
		if got, ok := industrySections[parent]; !ok || got != section {
			t.Errorf("%s code %q has parent %q = %q, %v; want same section", section, code, parent, got, ok)
		}
	}
}

// TestStatisticsPseudoDivisionFilter pins the source transformation: the
// statistics-bureau dataset carries development-zone pseudo divisions, and the
// embedded GB/T table must not accept them.
func TestStatisticsPseudoDivisionFilter(t *testing.T) {
	for _, code := range []string{"320571", "320576", "130171", "131071", "340176", "411571"} {
		if IsAdministrativeDivision(code) {
			t.Errorf("IsAdministrativeDivision(%q) = true; statistics-bureau development zones must not be accepted", code)
		}
	}
	for _, code := range []string{"110000", "110100", "110101", "419000", "419001", "659001", "370116", "510185"} {
		if !IsAdministrativeDivision(code) {
			t.Errorf("IsAdministrativeDivision(%q) = false; genuine divisions must be accepted", code)
		}
	}

	tests := []struct {
		code string
		want bool
	}{
		{"130171", true},  // 石家庄高新区, county code ending 71
		{"320576", true},  // 苏州工业园区, county code ending 76
		{"320571", true},  // the older statistics code for the same place
		{"320590", true},  // county code ending 90
		{"320599", true},  // county code ending 99
		{"419090", false}, // 省直辖县级 pattern: digits 3-4 are 90
		{"429090", false},
		{"419001", false},
		{"220184", false}, // a genuine county code ending 84
		{"110100", false},
		{"11000", false},
		{"11000a", false},
	}
	for _, tt := range tests {
		if got := isStatisticsPseudoDivision(tt.code); got != tt.want {
			t.Errorf("isStatisticsPseudoDivision(%q) = %v, want %v", tt.code, got, tt.want)
		}
	}

	codes, _, err := parseDivisions("# synthetic\n110000\n320590\n130171\n419001\n419090\n")
	if err != nil {
		t.Fatalf("parseDivisions: %v", err)
	}
	for _, dropped := range []string{"320590", "130171"} {
		if _, ok := codes[dropped]; ok {
			t.Errorf("parseDivisions kept %s, want it filtered as a statistics pseudo division", dropped)
		}
	}
	for _, keep := range []string{"419001", "419090", "110000"} {
		if _, ok := codes[keep]; !ok {
			t.Errorf("parseDivisions dropped %s, want it kept", keep)
		}
	}
}

func TestParseDivisionsRejectsMalformedData(t *testing.T) {
	for _, data := range []string{
		"11000",
		"1100000",
		"11000a",
		"990000",            // unknown province prefix
		"110000\n110000",    // duplicate
		"# only comments\n", // no codes
	} {
		if _, _, err := parseDivisions(data); err == nil {
			t.Errorf("parseDivisions(%q) = nil error, want error", data)
		}
	}
}

func TestParseIndustriesRejectsMalformedData(t *testing.T) {
	for _, data := range []string{
		"A 1",
		"A 01111",
		"A 01a",
		"Z 01",                // section outside A-T
		"01 A",                // fields reversed
		"A 01\nA 01",          // duplicate
		"A 011",               // missing 2-digit parent
		"A 01\nA 011\nB 0111", // 4-digit parent in another section
		"# only comments\n",   // no codes
	} {
		if _, _, err := parseIndustries(data); err == nil {
			t.Errorf("parseIndustries(%q) = nil error, want error", data)
		}
	}
}
