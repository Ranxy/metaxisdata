package model

import (
	"strings"

	"github.com/Ranxy/metaxisdata/backend/common"
)

type ObjectIdentifier struct {
	InstanceID string
	Database   string
	Schema     string
	Name       string
}

// FullName returns the object's dotted name, omitting empty qualifiers.
func (o ObjectIdentifier) FullName() string {
	sb := strings.Builder{}
	if o.Database != "" {
		_, _ = sb.WriteString(o.Database)
		_ = sb.WriteByte('.')
	}
	if o.Schema != "" {
		_, _ = sb.WriteString(o.Schema)
		_ = sb.WriteByte('.')
	}
	_, _ = sb.WriteString(o.Name)
	return sb.String()
}

func (o ObjectIdentifier) GUID() string {
	return common.BuildMetaGUID(o.InstanceID, o.Database, o.Schema, o.Name)
}

// NormalizeIdentifier returns an identifier's text with its quoting removed: a
// name written `a` or "a" names the same object as a, and a doubled quote inside
// it is one quote. Text that is not a quoted identifier comes back with its
// surrounding space trimmed and nothing else changed.
func NormalizeIdentifier(text string) string {
	text = strings.TrimSpace(text)
	if len(text) < 2 {
		return text
	}
	quote := text[0]
	if (quote != '`' && quote != '"') || text[len(text)-1] != quote {
		return text
	}
	inner := text[1 : len(text)-1]
	escapedQuote := strings.Repeat(string(quote), 2)
	return strings.ReplaceAll(inner, escapedQuote, string(quote))
}

func StrToObjectIdentifier(s string) ObjectIdentifier {
	list := strings.Split(s, ".")
	switch len(list) {
	case 1:
		return ObjectIdentifier{Name: list[0]}
	case 2:
		schema := list[0]
		return ObjectIdentifier{Schema: schema, Name: list[1]}
	case 3:
		database := list[0]
		schema := list[1]
		return ObjectIdentifier{Database: database, Schema: schema, Name: list[2]}
	case 4:
		instanceID := list[0]
		database := list[1]
		schema := list[2]
		return ObjectIdentifier{InstanceID: instanceID, Database: database, Schema: schema, Name: list[3]}
	default:
		return ObjectIdentifier{Name: s}
	}
}
