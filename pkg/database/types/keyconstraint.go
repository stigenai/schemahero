package types

import (
	"fmt"
	"strings"
)

type KeyConstraint struct {
	Name      string
	Columns   []string
	IsPrimary bool
}

func (k *KeyConstraint) Equals(other *KeyConstraint) bool {
	if k == nil && other == nil {
		return true
	}
	if k == nil || other == nil {
		return false
	}
	if k.IsPrimary != other.IsPrimary {
		return false
	}
	if len(k.Columns) != len(other.Columns) {
		return false
	}

	for i, column := range k.Columns {
		if column != other.Columns[i] {
			return false
		}
	}
	return true
}

func (k *KeyConstraint) GenerateName(tableName string) string {
	if k.Name != "" {
		return k.Name
	}
	// A constraint name lives in its table's schema and must not itself be
	// schema-qualified, so derive it from the bare table name (otherwise we
	// emit e.g. "global.foo_pkey", whose dot is a syntax error in
	// "add constraint <name> ...").
	bareName := bareTableName(tableName)
	if k.IsPrimary {
		return fmt.Sprintf("%s_pkey", bareName)
	}
	return fmt.Sprintf("%s_%s_key", bareName, strings.Join(k.Columns, "_"))
}
