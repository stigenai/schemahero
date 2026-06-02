package types

import (
	"fmt"
	"strings"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// ExclusionConstraintItem is the dialect-neutral form of one "element WITH
// operator" pair of an EXCLUDE constraint. Exactly one of Column or Expression
// is meaningful for a desired (spec) item; an introspected item recovered from
// pg_get_constraintdef carries the rendered element in Expression when it is not
// a bare column. The element() helper collapses the two into a single comparable
// string so both sides compare uniformly regardless of which field is set.
type ExclusionConstraintItem struct {
	Column     string
	Expression string
	Operator   string
}

// element returns the single element text for comparison: the bare column when
// Column is set, otherwise the raw expression. It is NOT used for emission (the
// DDL builder distinguishes column vs expression to quote correctly).
func (i ExclusionConstraintItem) element() string {
	if i.Column != "" {
		return i.Column
	}
	return i.Expression
}

// ExclusionConstraint is the introspected/desired form of a table-level EXCLUDE
// constraint. Items order is significant (it is the index element order, which
// is semantically meaningful for an EXCLUDE), so Equals compares Items
// positionally. With (storage params) is intentionally NOT part of Equals:
// introspection cannot cheaply recover it, and the FK/index diffs already match
// structurally rather than on every attribute.
type ExclusionConstraint struct {
	Name  string
	Using string
	Items []ExclusionConstraintItem
	Where string
	With  map[string]string
}

// normalizeUsing maps the empty string to the gist default and lowercases the
// access method so an omitted/uppercased method matches an introspected gist
// constraint and does not churn.
func normalizeUsing(using string) string {
	m := strings.ToLower(strings.TrimSpace(using))
	if m == "" {
		return "gist"
	}
	return m
}

// normalizeExclElement canonicalizes an EXCLUDE element / predicate / operator
// fragment via the shared CanonicalizeSQLExpr (lowercase, strip "::type" casts,
// drop all parentheses, collapse whitespace) — the SAME canonicalization the CHECK
// and index comparators use. This is load-bearing: pg_get_constraintdef renders an
// EXCLUDE's elements and WHERE predicate in PostgreSQL's canonical form (e.g. a
// bare "room_id is not null" predicate comes back as "(room_id IS NOT NULL)", and
// an expression element gains casts/parens), so a constraint authored in natural
// form would otherwise differ on every plan and force a guarded drop+recreate of
// the constraint and its backing index. Operators (= , &&) carry no parens/casts,
// so canonicalizing them is a harmless lowercase/whitespace collapse.
func normalizeExclElement(s string) string {
	return CanonicalizeSQLExpr(s)
}

func (e *ExclusionConstraint) Equals(other *ExclusionConstraint) bool {
	if e == nil && other == nil {
		return true
	}
	if e == nil || other == nil {
		return false
	}

	// Compare names: if both are set, they must match. If one is empty (e.g. from
	// a spec without an explicit name), skip the name check and rely on the
	// structural comparison, exactly like ForeignKey.Equals.
	if e.Name != "" && other.Name != "" && e.Name != other.Name {
		return false
	}

	if normalizeUsing(e.Using) != normalizeUsing(other.Using) {
		return false
	}

	if normalizeExclElement(e.Where) != normalizeExclElement(other.Where) {
		return false
	}

	if len(e.Items) != len(other.Items) {
		return false
	}

	// Items are compared positionally: an EXCLUDE's element order is semantically
	// significant, unlike a unique index's column set.
	for i := range e.Items {
		if normalizeExclElement(e.Items[i].element()) != normalizeExclElement(other.Items[i].element()) {
			return false
		}
		if normalizeExclElement(e.Items[i].Operator) != normalizeExclElement(other.Items[i].Operator) {
			return false
		}
	}

	return true
}

// PostgresqlSchemaExclusionConstraintToExclusionConstraint converts the API spec
// form into the dialect-neutral domain form used by the diff.
func PostgresqlSchemaExclusionConstraintToExclusionConstraint(schemaExclusion *schemasv1alpha4.PostgresqlTableExclusionConstraint) *ExclusionConstraint {
	exclusionConstraint := ExclusionConstraint{
		Name:  schemaExclusion.Name,
		Using: schemaExclusion.Using,
		Where: schemaExclusion.Where,
		With:  schemaExclusion.With,
	}

	for _, item := range schemaExclusion.Items {
		exclusionConstraint.Items = append(exclusionConstraint.Items, ExclusionConstraintItem{
			Column:     item.Column,
			Expression: item.Expression,
			Operator:   item.Operator,
		})
	}

	return &exclusionConstraint
}

// GeneratePostgresqlExclusionConstraintName returns the constraint name to use
// for a desired EXCLUDE. If the user set Name explicitly that is returned
// verbatim; otherwise a deterministic "<bareTable>_<elements>_excl" name is
// derived.
//
// It MUST use bareTableName so a "schema.table" never leaks a dot into the
// identifier (a dot would make "add constraint <name> ..." invalid SQL), the
// same rule the FK, key-constraint, and check-constraint name generators follow.
func GeneratePostgresqlExclusionConstraintName(tableName string, c *schemasv1alpha4.PostgresqlTableExclusionConstraint) string {
	if c.Name != "" {
		return c.Name
	}

	fragments := make([]string, 0, len(c.Items))
	for _, item := range c.Items {
		element := item.Column
		if element == "" {
			element = item.Expression
		}
		fragments = append(fragments, sanitizeIdentFragment(element))
	}

	return capPostgresIdentifier(fmt.Sprintf("%s_%s_excl", bareTableName(tableName), strings.Join(fragments, "_")))
}
