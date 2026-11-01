package dataquality

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Time variables are the only templating a check definition has. They are a fixed
// whitelist rather than an expression language: ${now}, ${today} and ${yesterday},
// each with at most one signed offset in minutes, hours or days (${today-7d},
// ${now-30m}). They resolve from the execution's own start instant, so one stored
// rule renders a different window on every run.
//
// Exactly two places resolve them: a filter clause value that consists of a single
// token, and custom SQL text. A filter value stays a bound argument like any other
// value. In custom SQL the rendered value is written as a server-generated ISO
// literal (digits, spaces, dashes and colons only), never user text: a token there
// may sit inside a comment or a quoted string, where a bound placeholder would
// either be inert while still consuming an argument or collide with a $1/@p1 the
// user wrote, so the generated literal is the only rendering that stays correct in
// every position.

const (
	// maxVariableOffsetDays bounds one offset, so a typo cannot produce an instant
	// far outside any table's range. It is checked before a unit is scaled, which is
	// also what keeps a huge digit string from wrapping.
	maxVariableOffsetDays = 366
	// dateLayout and timestampLayout are the two renderings. A variable that lands
	// on local midnight renders as a date (that is what ${today} means to a
	// reader); anything else renders as a timestamp. Every supported engine casts
	// between the two in a comparison.
	dateLayout      = "2006-01-02"
	timestampLayout = "2006-01-02 15:04:05"
)

// timeVariablePattern matches one whole token: a lower-case name and at most one
// signed integer offset with its unit. A malformed token does not match, which is
// how an unknown variable is caught instead of being compared literally.
var timeVariablePattern = regexp.MustCompile(`\$\{([a-z]+)((?:[+-][0-9]+[mhd])?)\}`)

// TimeVariables is the compile-time context a rule's time variables resolve in:
// the instant the statement is built for, and the workspace's check time zone. The
// zero value carries no instant, and a rule that uses a variable is refused rather
// than compiled against a zero time.
type TimeVariables struct {
	At       time.Time
	Location *time.Location
}

// NewTimeVariables builds a context, defaulting the zone to UTC.
func NewTimeVariables(at time.Time, location *time.Location) TimeVariables {
	if location == nil {
		location = time.UTC
	}
	return TimeVariables{At: at, Location: location}
}

// referenceTimeVariables resolves tokens for save-time validation. Token rendering
// is all the column-type check needs and the concrete instant is irrelevant for
// it, so validation does not have to thread a clock through every entry point.
func referenceTimeVariables() TimeVariables {
	return NewTimeVariables(time.Unix(0, 0).UTC(), time.UTC)
}

// ContainsTimeVariable reports whether text mentions a variable at all, complete or
// malformed. It is the save-time gate for config fields that never resolve one.
func ContainsTimeVariable(text string) bool {
	return strings.Contains(text, "${")
}

// ResolveTimeVariable renders one whole token such as "${today-7d}". An unknown
// name or a malformed offset is a save-time error: a typo must not silently become
// a literal string compared with the column.
func ResolveTimeVariable(token string, vars TimeVariables) (string, error) {
	trimmed := strings.TrimSpace(token)
	match := timeVariablePattern.FindStringSubmatch(trimmed)
	if match == nil || match[0] != trimmed {
		return "", invalidConfig("%q is not a supported time variable", token)
	}
	if vars.At.IsZero() {
		return "", common.Errorf(common.Internal, "the execution has no start time to resolve %q against", token)
	}
	return renderTimeVariable(match[1], match[2], vars)
}

// renderTimeVariable resolves a matched token's name and offset. The name decides
// the base instant (a local midnight for today/yesterday, the execution start for
// now) and the offset shifts it.
func renderTimeVariable(name, offset string, vars TimeVariables) (string, error) {
	location := vars.Location
	if location == nil {
		location = time.UTC
	}
	local := vars.At.In(location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	var at time.Time
	switch name {
	case "now":
		at = local
	case "today":
		at = midnight
	case "yesterday":
		at = midnight.AddDate(0, 0, -1)
	default:
		return "", invalidConfig("%q is not a supported time variable; supported names are now, today and yesterday", "${"+name+"}")
	}
	if offset != "" {
		amount, unit, err := parseVariableOffset(offset)
		if err != nil {
			return "", err
		}
		switch unit {
		case 'd':
			at = at.AddDate(0, 0, amount)
		case 'h':
			at = at.Add(time.Duration(amount) * time.Hour)
		case 'm':
			at = at.Add(time.Duration(amount) * time.Minute)
		default:
			// parseVariableOffset refuses any other unit before it gets here.
			return "", invalidConfig("time variable offset %q must end in m, h or d", offset)
		}
	}
	if at.Hour() == 0 && at.Minute() == 0 && at.Second() == 0 && at.Nanosecond() == 0 {
		return at.Format(dateLayout), nil
	}
	return at.Format(timestampLayout), nil
}

// parseVariableOffset parses the "+N unit" tail of a token and returns its signed
// amount and unit. A day offset moves the calendar (AddDate) while an hour or
// minute offset moves the instant, so "${today-1d}" and "${yesterday}" agree even on
// a daylight-saving transition. The amount is bounded in day-equivalents before it
// is scaled, which both rejects a typo and keeps the arithmetic from overflowing.
func parseVariableOffset(offset string) (int, byte, error) {
	sign := 1
	if strings.HasPrefix(offset, "-") {
		sign = -1
	}
	body := offset[1:]
	unit := body[len(body)-1]
	amount, err := strconv.Atoi(body[:len(body)-1])
	if err != nil || amount < 0 {
		return 0, 0, invalidConfig("time variable offset %q is not a whole number of minutes, hours or days", offset)
	}
	var days int
	switch unit {
	case 'd':
		days = amount
	case 'h':
		days = (amount + 23) / 24
	case 'm':
		days = (amount + 1439) / 1440
	default:
		return 0, 0, invalidConfig("time variable offset %q must end in m, h or d", offset)
	}
	if days > maxVariableOffsetDays {
		return 0, 0, invalidConfig("time variable offset %q is larger than the %d-day limit", offset, maxVariableOffsetDays)
	}
	return sign * amount, unit, nil
}

// ResolveFilterVariables replaces each clause value that is a single time variable
// with its rendering. A value that mixes a token with other text is refused: the
// filter binds typed values, so "prefix-${today}" is a mistake rather than a
// string to compare. The returned filter is a copy; the caller's is untouched.
func ResolveFilterVariables(filter Filter, vars TimeVariables) (Filter, error) {
	if len(filter.Clauses) == 0 {
		return filter, nil
	}
	resolved := Filter{Clauses: make([]FilterClause, len(filter.Clauses))}
	for index, clause := range filter.Clauses {
		resolved.Clauses[index] = clause
		values := make([]string, len(clause.Values))
		for valueIndex, value := range clause.Values {
			trimmed := strings.TrimSpace(value)
			switch {
			case !ContainsTimeVariable(trimmed):
				values[valueIndex] = value
			case strings.HasPrefix(trimmed, "${") && strings.HasSuffix(trimmed, "}"):
				rendered, err := ResolveTimeVariable(trimmed, vars)
				if err != nil {
					return Filter{}, err
				}
				values[valueIndex] = rendered
			default:
				return Filter{}, invalidConfig("filter value %q mixes a time variable with other text; a filter value is either a literal or one time variable", value)
			}
		}
		resolved.Clauses[index].Values = values
	}
	return resolved, nil
}

// RenderCustomSQLVariables replaces every token in custom SQL with a quoted,
// server-generated ISO literal. The value is built here from a timestamp layout
// (digits, dashes, colons and one space), never from user text, so it cannot alter
// the statement; and being inert text it stays harmless inside a comment or a
// quoted string and cannot collide with a $1/@p1 the user wrote. A filter value in
// the same rule is still bound as a parameter like any other value.
func RenderCustomSQLVariables(statement string, vars TimeVariables) (string, error) {
	return replaceTimeVariables(statement, func(token string) (string, error) {
		value, err := ResolveTimeVariable(token, vars)
		if err != nil {
			return "", err
		}
		return "'" + value + "'", nil
	})
}

// replaceTimeVariables rewrites every token in text with render's replacement and
// refuses text that still mentions one afterwards, which is how a malformed token
// ("${today" / "${Today}") is reported instead of reaching the parser.
func replaceTimeVariables(text string, render func(token string) (string, error)) (string, error) {
	var builder strings.Builder
	rest := text
	for {
		loc := timeVariablePattern.FindStringIndex(rest)
		if loc == nil {
			_, _ = builder.WriteString(rest)
			break
		}
		_, _ = builder.WriteString(rest[:loc[0]])
		replacement, err := render(rest[loc[0]:loc[1]])
		if err != nil {
			return "", err
		}
		_, _ = builder.WriteString(replacement)
		rest = rest[loc[1]:]
	}
	rendered := builder.String()
	if ContainsTimeVariable(rendered) {
		return "", invalidConfig("the statement contains a time variable this build does not support; supported names are now, today and yesterday with an optional -Nm/-Nh/-Nd offset")
	}
	return rendered, nil
}

// customSQLForParsing renders the time variables of a stored custom SQL statement
// for the engine's parser. It uses the same rendering the execution will use, so a
// statement the save path accepts is the statement the executor runs. A token
// written inside a single-quoted literal still fails to parse (the literal becomes
// two adjacent quotes), which is the intended refusal.
func customSQLForParsing(statement string) (string, error) {
	return RenderCustomSQLVariables(statement, referenceTimeVariables())
}

// namedText is one config text field, for the refusal below.
type namedText struct {
	name   string
	values []string
}

// rejectUnexpectedTimeVariables refuses a variable token anywhere a value is
// compared literally. Only the filter's clause values and custom SQL text resolve
// variables; everywhere else a token would silently compare as text, which is
// never what it means.
func rejectUnexpectedTimeVariables(config *storepb.QualityRuleConfig) error {
	for _, field := range ruleConfigTextFields(config) {
		for _, value := range field.values {
			if ContainsTimeVariable(value) {
				return invalidConfig("%s does not resolve time variables; they are supported in filter values and custom SQL only", field.name)
			}
		}
	}
	return nil
}

// ruleConfigTextFields lists the user-typed value fields a config can carry. The
// filter and custom SQL are deliberately absent: they do resolve variables.
func ruleConfigTextFields(config *storepb.QualityRuleConfig) []namedText {
	fields := []namedText{
		{"null_values", config.GetNullPercent().GetNullValues()},
		{"accepted values", config.GetAcceptedValues().GetValues()},
		{"range bounds", []string{config.GetRange().GetMin(), config.GetRange().GetMax()}},
		{"regex pattern", []string{config.GetRegex().GetPattern()}},
		{"CEL expression", []string{config.GetCelExpression().GetExpression()}},
	}
	if drift := config.GetSchemaDrift(); drift != nil {
		for _, column := range drift.GetExpectedColumns() {
			fields = append(fields, namedText{"schema drift snapshot", []string{column.GetName(), column.GetType()}})
		}
	}
	return fields
}
