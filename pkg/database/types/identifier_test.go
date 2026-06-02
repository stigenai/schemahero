package types

import (
	"strings"
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"

	"github.com/stretchr/testify/assert"
)

// Test_capPostgresIdentifier_shortNamesUnchanged verifies the cap is a no-op for
// names that already fit, so existing short generated names are byte-identical to
// before this fix (no spurious churn on already-deployed objects).
func Test_capPostgresIdentifier_shortNamesUnchanged(t *testing.T) {
	short := "users_email_check"
	assert.Equal(t, short, capPostgresIdentifier(short))

	exactly63 := strings.Repeat("a", maxPostgresIdentifierLen)
	assert.Len(t, exactly63, 63)
	assert.Equal(t, exactly63, capPostgresIdentifier(exactly63))
}

// Test_capPostgresIdentifier_longNamesCappedStable is the unit-level proof behind
// HIGH-1: a name longer than 63 bytes must be capped to <=63 bytes and the cap
// must be STABLE (same input -> same output) so a second reconcile generates the
// identical name and emits no DDL (no ADD-then-DROP wedge).
func Test_capPostgresIdentifier_longNamesCappedStable(t *testing.T) {
	long := strings.Repeat("x", 200) + "_check"

	first := capPostgresIdentifier(long)
	second := capPostgresIdentifier(long)

	assert.LessOrEqual(t, len(first), maxPostgresIdentifierLen, "capped name must fit Postgres' 63-byte limit")
	assert.Equal(t, first, second, "cap must be deterministic across reconciles")
}

// Test_capPostgresIdentifier_distinctLongNamesStayDistinct proves the uniqueness
// preservation: two different long names that SHARE a 63-byte prefix must produce
// DIFFERENT capped names (a naive prefix-slice truncation would alias them to the
// same identifier and cause a constraint-name collision).
func Test_capPostgresIdentifier_distinctLongNamesStayDistinct(t *testing.T) {
	prefix := strings.Repeat("c", 80)
	a := prefix + "_alpha_check"
	b := prefix + "_beta_check"

	capA := capPostgresIdentifier(a)
	capB := capPostgresIdentifier(b)

	assert.NotEqual(t, capA, capB, "distinct long names sharing a prefix must not collide after capping")
	assert.LessOrEqual(t, len(capA), maxPostgresIdentifierLen)
	assert.LessOrEqual(t, len(capB), maxPostgresIdentifierLen)
}

// Test_GeneratePostgresqlCheckName_capped wires the cap through the real CHECK
// name generator: a long expression must yield a <=63-byte name, stably.
func Test_GeneratePostgresqlCheckName_capped(t *testing.T) {
	c := &schemasv1alpha4.PostgresqlTableCheckConstraint{
		// A long expression with no explicit Name forces the fallback generator
		// down the long-name path.
		Expression: "very_long_column_name_one + very_long_column_name_two + very_long_column_name_three > 0",
	}

	name := GeneratePostgresqlCheckName("orders", c)
	assert.LessOrEqual(t, len(name), maxPostgresIdentifierLen)
	assert.Equal(t, name, GeneratePostgresqlCheckName("orders", c), "generated name must be stable")
}

// Test_GeneratePostgresqlExclusionConstraintName_capped wires the cap through the
// real EXCLUDE name generator.
func Test_GeneratePostgresqlExclusionConstraintName_capped(t *testing.T) {
	c := &schemasv1alpha4.PostgresqlTableExclusionConstraint{
		Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{
			{Column: "very_long_column_name_used_for_the_exclusion_constraint_element_one", Operator: "="},
			{Column: "very_long_column_name_used_for_the_exclusion_constraint_element_two", Operator: "&&"},
		},
	}

	name := GeneratePostgresqlExclusionConstraintName("reservations", c)
	assert.LessOrEqual(t, len(name), maxPostgresIdentifierLen)
	assert.Equal(t, name, GeneratePostgresqlExclusionConstraintName("reservations", c), "generated name must be stable")
}

// Test_GeneratePostgresqlIndexName_capped wires the cap through the real index
// name generator.
func Test_GeneratePostgresqlIndexName_capped(t *testing.T) {
	idx := &schemasv1alpha4.PostgresqlTableIndex{
		Columns: []string{
			"very_long_column_name_one",
			"very_long_column_name_two",
			"very_long_column_name_three",
			"very_long_column_name_four",
		},
	}

	name := GeneratePostgresqlIndexName("events", idx)
	assert.LessOrEqual(t, len(name), maxPostgresIdentifierLen)
	assert.Equal(t, name, GeneratePostgresqlIndexName("events", idx), "generated name must be stable")
}
