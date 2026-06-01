package types

import (
	"fmt"
	"strings"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// CheckConstraint is the introspected form of a table-level CHECK constraint.
// Expression holds the inner boolean recovered from pg_get_constraintdef (the
// surrounding "CHECK (...)" wrapper stripped); because PostgreSQL always
// re-parenthesizes and casts, it is almost never byte-equal to the user's raw
// spec expression, so the diff compares it only via a normalized equivalence
// check (see plugins/postgres/lib/checkconstraint.go), never byte-for-byte.
type CheckConstraint struct {
	Name       string
	Expression string
}

// GeneratePostgresqlCheckName returns the constraint name to use for a desired
// CHECK. If the user set Name explicitly that is returned verbatim; otherwise a
// deterministic "<bareTable>_<sanitized-expr>_check" name is derived.
//
// It MUST use bareTableName so a "schema.table" never leaks a dot into the
// identifier (a dot would make "add constraint <name> ..." invalid SQL), the
// same rule the FK and key-constraint name generators follow.
func GeneratePostgresqlCheckName(tableName string, c *schemasv1alpha4.PostgresqlTableCheckConstraint) string {
	if c.Name != "" {
		return c.Name
	}

	return fmt.Sprintf("%s_%s_check", bareTableName(tableName), sanitizeIdentFragment(c.Expression))
}

// sanitizeIdentFragment turns an arbitrary expression into a safe identifier
// fragment: lowercased, every run of non-[a-z0-9] characters collapsed to a
// single underscore, and surrounding underscores trimmed. It is only ever used
// to build a fallback name when the user did not set one; an explicit Name is
// always preferred because expression-derived names are fragile.
func sanitizeIdentFragment(s string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteRune('_')
			prevUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}
