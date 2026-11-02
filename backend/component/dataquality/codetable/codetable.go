// Package codetable provides offline lookups for Chinese national standard
// codes used by data-quality checks.
//
// Two tables are compiled into the binary and parsed once at init; there is no
// runtime network access.
//
//   - division_data.txt: 3226 six-digit GB/T 2260 administrative division codes,
//     derived from the province/city/county lists of
//     https://github.com/modood/Administrative-divisions-of-China release 2.7.0
//     (division data as of 2023-06-30; raw snapshot retrieved 2026-11-02).
//     County codes are kept as published, province "XX0000" and city "XXXX00"
//     aggregate rows are derived, and the 710000/810000/820000 province codes
//     are added because the source's Taiwan/Hong Kong/Macao district lists carry
//     no GB/T codes. The source is a statistics-bureau dataset, so its 124
//     development-zone pseudo divisions are removed: county codes whose last two
//     digits are 71-79 (for example 130171 石家庄高新区 and 320576 苏州工业园区),
//     and codes whose last two digits are 90-99 except the 省直辖县级 aggregate
//     pattern (digits 3-4 are 90, for example 419000/419001). GB/T 2260 carries
//     no 71-79 county codes, so the rule is exact for this source.
//   - industry_data.txt: 1950 GB/T 4754-2017 industry classification numeric
//     codes (97 大类, 473 中类, 1380 小类), including 第1号修改单 (2019), stored
//     as "<section> <numeric>" pairs and parsed from the standard text embedded
//     in https://github.com/ufoym/industry-map (snapshot retrieved 2026-11-02).
//     Names and notes are dropped.
//
// Each data file begins with '#'-prefixed header lines recording the same
// provenance plus the exact transformation and cross-checks. A malformed
// table is a build defect, so parsing failures panic during init.
//
// Updating the data means re-fetching the sources, re-running the
// transformation and re-verifying with the data-sanity tests in
// codetable_test.go; do not edit the tables by hand.
package codetable

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

//go:embed division_data.txt
var divisionData string

//go:embed industry_data.txt
var industryData string

// provincePrefixList lists the 34 province-level prefixes of GB/T 2260.
var provincePrefixList = []string{
	"11", "12", "13", "14", "15",
	"21", "22", "23",
	"31", "32", "33", "34", "35", "36", "37",
	"41", "42", "43", "44", "45", "46",
	"50", "51", "52", "53", "54",
	"61", "62", "63", "64", "65",
	"71", "81", "82",
}

var allowedProvincePrefixes = func() map[string]struct{} {
	m := make(map[string]struct{}, len(provincePrefixList))
	for _, p := range provincePrefixList {
		m[p] = struct{}{}
	}
	return m
}()

var (
	divisionCodes       = map[string]struct{}{}
	provincePrefixes    = map[string]struct{}{}
	industrySections    = map[string]string{}
	industrySectionsSet = map[byte]struct{}{}
)

func init() {
	divisions, prefixes, err := parseDivisions(divisionData)
	if err != nil {
		panic(fmt.Sprintf("codetable: invalid division_data.txt: %v", err))
	}
	divisionCodes = divisions
	provincePrefixes = prefixes

	industries, sections, err := parseIndustries(industryData)
	if err != nil {
		panic(fmt.Sprintf("codetable: invalid industry_data.txt: %v", err))
	}
	industrySections = industries
	industrySectionsSet = sections
}

// IsAdministrativeDivision reports whether code is a 6-digit GB/T 2260
// administrative division code present in the embedded table.
func IsAdministrativeDivision(code string) bool {
	_, ok := divisionCodes[code]
	return ok
}

// IsProvincePrefix reports whether code is a 2-digit province-level prefix
// present in the embedded division table.
func IsProvincePrefix(code string) bool {
	_, ok := provincePrefixes[code]
	return ok
}

// IsIndustryClassification reports whether code is a GB/T 4754-2017 code:
// an optional section letter (A-T, case-insensitive) followed by 2, 3 or 4
// digits, where the numeric code exists in the embedded table and (when a
// letter is given) belongs to that section. A bare section letter is also
// accepted.
func IsIndustryClassification(code string) bool {
	if code == "" {
		return false
	}

	var letter byte
	numeric := code
	switch c := code[0]; {
	case c >= 'A' && c <= 'T':
		letter = c
		numeric = code[1:]
	case c >= 'a' && c <= 't':
		letter = c - ('a' - 'A')
		numeric = code[1:]
	}

	if numeric == "" {
		if letter == 0 {
			return false
		}
		_, ok := industrySectionsSet[letter]
		return ok
	}

	if len(numeric) < 2 || len(numeric) > 4 {
		return false
	}
	for i := 0; i < len(numeric); i++ {
		if numeric[i] < '0' || numeric[i] > '9' {
			return false
		}
	}

	section, ok := industrySections[numeric]
	if !ok {
		return false
	}
	return letter == 0 || section[0] == letter
}

// parseDivisions parses the embedded division file. It returns the accepted
// codes and the province prefixes derived from them.
func parseDivisions(data string) (map[string]struct{}, map[string]struct{}, error) {
	codes := make(map[string]struct{})
	prefixes := make(map[string]struct{})

	for n, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) != 6 {
			return nil, nil, fmt.Errorf("line %d: %q is not a 6-digit code", n+1, line)
		}
		for i := 0; i < len(line); i++ {
			if line[i] < '0' || line[i] > '9' {
				return nil, nil, fmt.Errorf("line %d: %q is not a 6-digit code", n+1, line)
			}
		}
		// The source is a statistics-bureau dataset; drop its development-zone
		// pseudo divisions so the table carries genuine GB/T 2260 codes only.
		if isStatisticsPseudoDivision(line) {
			continue
		}
		if _, ok := allowedProvincePrefixes[line[:2]]; !ok {
			return nil, nil, fmt.Errorf("line %d: %q has unknown province prefix", n+1, line)
		}
		if _, ok := codes[line]; ok {
			return nil, nil, fmt.Errorf("line %d: duplicate code %q", n+1, line)
		}
		codes[line] = struct{}{}
		prefixes[line[:2]] = struct{}{}
	}

	if len(codes) == 0 {
		return nil, nil, fmt.Errorf("no codes")
	}
	return codes, prefixes, nil
}

// isStatisticsPseudoDivision reports whether code is a statistics-bureau
// development-zone pseudo division rather than a GB/T 2260 administrative
// division. Such a code is county-level: its last two digits are 71-79 (for
// example 130171 石家庄高新区) or 90-99 (for example 320590), except for the
// 省直辖县级 aggregate pattern whose digits 3-4 are 90 (for example 419000).
func isStatisticsPseudoDivision(code string) bool {
	if len(code) != 6 {
		return false
	}
	if code[2:4] == "90" {
		return false
	}
	n, err := strconv.Atoi(code[4:])
	if err != nil {
		return false
	}
	return (n >= 71 && n <= 79) || (n >= 90 && n <= 99)
}

// parseIndustries parses the embedded industry file, which stores one
// "<section> <numeric-code>" pair per line. It returns numeric code to owning
// section and the set of present section letters.
func parseIndustries(data string) (map[string]string, map[byte]struct{}, error) {
	sections := make(map[string]string)
	sectionLetters := make(map[byte]struct{})

	for n, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, nil, fmt.Errorf("line %d: want \"<section> <code>\", got %q", n+1, line)
		}
		if len(fields[0]) != 1 || fields[0][0] < 'A' || fields[0][0] > 'T' {
			return nil, nil, fmt.Errorf("line %d: %q is not a section letter A-T", n+1, fields[0])
		}
		number := fields[1]
		if len(number) < 2 || len(number) > 4 {
			return nil, nil, fmt.Errorf("line %d: %q is not a 2, 3 or 4 digit code", n+1, number)
		}
		for i := 0; i < len(number); i++ {
			if number[i] < '0' || number[i] > '9' {
				return nil, nil, fmt.Errorf("line %d: %q is not a 2, 3 or 4 digit code", n+1, number)
			}
		}
		if previous, ok := sections[number]; ok {
			return nil, nil, fmt.Errorf("line %d: duplicate code %q (already under %s)", n+1, number, previous)
		}
		sections[number] = fields[0]
		sectionLetters[fields[0][0]] = struct{}{}
	}

	if len(sections) == 0 {
		return nil, nil, fmt.Errorf("no codes")
	}

	// Every 中类/小类 must hang off a code of its own section.
	for code, section := range sections {
		var parent string
		switch len(code) {
		case 3:
			parent = code[:2]
		case 4:
			parent = code[:3]
		default:
			continue
		}
		parentSection, ok := sections[parent]
		if !ok {
			return nil, nil, fmt.Errorf("%s code %q is missing parent %q", section, code, parent)
		}
		if parentSection != section {
			return nil, nil, fmt.Errorf("%s code %q has parent %q under %s", section, code, parent, parentSection)
		}
	}

	return sections, sectionLetters, nil
}
