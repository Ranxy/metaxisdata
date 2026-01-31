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

// nolint:revive
func (o ObjectIdentifier) FullName() string {
	sb := strings.Builder{}
	if o.Database != "" {
		sb.WriteString(o.Database)
		sb.WriteByte('.')
	}
	if o.Schema != "" {
		sb.WriteString(o.Schema)
		sb.WriteByte('.')
	}
	sb.WriteString(o.Name)
	return sb.String()
}

func (o ObjectIdentifier) GUID() string {
	return strings.Join([]string{o.InstanceID, o.Database, o.Schema, o.Name}, common.MetaGUIDSplit)
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
