package codetable

import (
	"testing"
)

func TestIsAdministrativeDivision(t *testing.T) {
	t.Parallel()

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
		{"added Taiwan province", "710000", true},
		{"added Hong Kong province", "810000", true},
		{"added Macao province", "820000", true},
		{"genuine county", "370116", true},
		{"genuine county second", "510185", true},
		{"empty", "", false},
		{"five digits", "11000", false},
		{"seven digits", "1100000", false},
		{"unknown code", "999999", false},
		{"letters", "abcdef", false},
		{"trailing letter", "11000a", false},
		{"statistics development zone", "320571", false},
		{"statistics development zone ending 76", "320576", false},
		{"statistics development zone ending 71", "130171", false},
		{"statistics development zone second", "131071", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := IsAdministrativeDivision(test.code); got != test.want {
				t.Errorf("IsAdministrativeDivision(%q) = %v, want %v", test.code, got, test.want)
			}
		})
	}
}

func TestIsProvincePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		want   bool
	}{
		{"province", "11", true},
		{"municipality", "31", true},
		{"autonomous region", "65", true},
		{"special administrative region", "81", true},
		{"unknown prefix", "16", false},
		{"zero prefix", "00", false},
		{"unassigned prefix", "99", false},
		{"one digit", "9", false},
		{"three digits", "110", false},
		{"empty", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := IsProvincePrefix(test.prefix); got != test.want {
				t.Errorf("IsProvincePrefix(%q) = %v, want %v", test.prefix, got, test.want)
			}
		})
	}
}

func TestIsIndustryClassification(t *testing.T) {
	t.Parallel()

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
		{"missing numeric code", "I6510", false},
		{"wrong section for division", "A39", false},
		{"wrong section for group", "C01", false},
		{"wrong section for class", "A3911", false},
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
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := IsIndustryClassification(test.code); got != test.want {
				t.Errorf("IsIndustryClassification(%q) = %v, want %v", test.code, got, test.want)
			}
		})
	}
}

// TestDivisionDataSanity re-asserts the pinned snapshot's shape: 3226 division
// codes over all 34 province-level prefixes, with the statistics-bureau
// development-zone pseudo divisions still filtered out. A malformed line or a
// duplicate cannot reach this test — init panics first — so these are the
// properties a data refresh can silently change.
func TestDivisionDataSanity(t *testing.T) {
	t.Parallel()

	// The pinned release's genuine divisions, its derived province "XX0000" and
	// city "XXXX00" aggregates, and the added 710000/810000/820000.
	if got := len(divisionCodes); got != 3226 {
		t.Fatalf("division code count = %d, want the pinned snapshot's 3226", got)
	}
	if got := len(provincePrefixes); got != len(provincePrefixList) {
		t.Fatalf("province prefix count = %d, want %d", got, len(provincePrefixList))
	}

	// GB/T 2260 has no county codes ending 71-79, and 90-99 appears only in the
	// 省直辖县级 aggregate pattern whose digits 3-4 are 90. A refresh that
	// reintroduced a statistics-bureau development zone would fail here.
	for code := range divisionCodes {
		if code[2:4] == "90" {
			continue
		}
		if last := code[4:]; (last >= "71" && last <= "79") || (last >= "90" && last <= "99") {
			t.Errorf("loaded table contains %q, whose county segment ends %s like a statistics pseudo division", code, last)
		}
	}
}

// TestIndustryDataSanity re-asserts the pinned snapshot's shape: the published
// 97 大类, 473 中类 and 1380 小类 codes across the 20 sections A-T. As with the
// division table, init already panics on a malformed or inconsistent line, so
// only the counts a data refresh can silently change are checked here.
func TestIndustryDataSanity(t *testing.T) {
	t.Parallel()

	byLength := make(map[int]int, 3)
	sections := make(map[byte]struct{}, 20)
	for code, section := range industrySections {
		byLength[len(code)]++
		sections[section[0]] = struct{}{}
	}

	if got := len(industrySections); got != 1950 {
		t.Fatalf("industry code count = %d, want the pinned snapshot's 1950", got)
	}
	for length, want := range map[int]int{2: 97, 3: 473, 4: 1380} {
		if got := byLength[length]; got != want {
			t.Errorf("%d-digit code count = %d, want %d", length, got, want)
		}
	}
	for c := byte('A'); c <= 'T'; c++ {
		if _, ok := sections[c]; !ok {
			t.Errorf("section %c has no codes", c)
		}
	}
	if got := len(sections); got != 20 {
		t.Errorf("section count = %d, want 20", got)
	}
}

// TestParseDivisionsDropsStatisticsPseudoDivisions pins the source transformation:
// statistics-bureau development zones (county tails 71-79 and 90-99) are dropped,
// while the 省直辖县级 aggregate rows and genuine county codes are kept.
func TestParseDivisionsDropsStatisticsPseudoDivisions(t *testing.T) {
	t.Parallel()

	codes, _, err := parseDivisions("# synthetic\n110000\n320590\n320599\n320576\n130171\n419001\n419090\n429090\n220184\n")
	if err != nil {
		t.Fatalf("parseDivisions: %v", err)
	}
	for _, dropped := range []string{"320590", "320599", "320576", "130171"} {
		if _, ok := codes[dropped]; ok {
			t.Errorf("parseDivisions kept %s, want it filtered as a statistics pseudo division", dropped)
		}
	}
	for _, kept := range []string{"419001", "419090", "429090", "220184", "110000"} {
		if _, ok := codes[kept]; !ok {
			t.Errorf("parseDivisions dropped %s, want it kept", kept)
		}
	}
}

func TestParseDivisionsRejectsMalformedData(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
