package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test_CanonicalizeSQLExpr_naturalEqualsCanonical is the unit-level proof behind
// the HIGH-2 fix: a fragment authored in NATURAL form must canonicalize to the
// SAME string PostgreSQL renders back via pg_get_expr / pg_get_constraintdef. If
// these did not match, an expression/partial index or EXCLUDE predicate would
// drop+recreate on every plan.
func Test_CanonicalizeSQLExpr_naturalEqualsCanonical(t *testing.T) {
	tests := []struct {
		name      string
		natural   string // as a human would author it in a spec
		canonical string // as pg_get_expr / pg_get_constraintdef renders it back
	}{
		{
			name:      "partial-index predicate with empty-string comparison",
			natural:   "phone <> ''",
			canonical: "((phone)::text <> ''::text)",
		},
		{
			name:      "functional-index expression lower(col)",
			natural:   "lower(email)",
			canonical: "lower((email)::text)",
		},
		{
			name:      "EXCLUDE predicate IS NOT NULL gains wrapping parens",
			natural:   "room_id is not null",
			canonical: "(room_id IS NOT NULL)",
		},
		{
			name:      "numeric comparison gains a ::numeric cast",
			natural:   "amount > 0",
			canonical: "(amount > (0)::numeric)",
		},
		{
			name:      "varchar comparison gains ::text casts on both sides",
			natural:   "status = 'a'",
			canonical: "((status)::text = 'a'::character varying)",
		},
		{
			// PostgreSQL quotes a negative numeric literal before casting it
			// ("-273" -> "'-273'::integer"). A user writes it unquoted, so the
			// canonicalizer must unquote the cast literal or a CHECK/index/EXCLUDE
			// using a negative number would churn on every plan.
			name:      "negative numeric literal quoted+cast by postgres",
			natural:   "temperature >= -273",
			canonical: "(temperature >= '-273'::integer)",
		},
		{
			name:      "decimal numeric literal quoted+cast by postgres",
			natural:   "rate <= 99.5",
			canonical: "(rate <= '99.5'::numeric)",
		},
		{
			// pg_get_constraintdef(oid, true) (pretty) renders the cast WITHOUT
			// wrapping parens, so an "::integer" is immediately followed by "AND".
			// The cast skipper must stop at "and" (not swallow "and temperature ...")
			// or the whole tail of the predicate is eaten and the CHECK churns.
			name:      "unparenthesized cast followed by AND (pretty pg_get_constraintdef)",
			natural:   "temperature_celsius >= -273 and temperature_celsius <= 1000000",
			canonical: "temperature_celsius >= '-273'::integer AND temperature_celsius <= 1000000",
		},
		{
			// Multi-word type after an unparenthesized cast must still be consumed
			// fully ("character varying"), then stop at the operator.
			name:      "unparenthesized character varying cast followed by operator",
			natural:   "name = 'x' and id > 0",
			canonical: "(name)::text = 'x'::character varying and id > 0",
		},
		{
			name:      "case-insensitive boolean operators",
			natural:   "a AND b",
			canonical: "(a and b)",
		},
		{
			// PostgreSQL renders list/array separators as ", " (comma + space) and
			// adds a ::text cast to each element; a spec authoring "ARRAY['a','b']"
			// (no spaces) must still canonicalize equal, or a "= ANY (ARRAY[...])"
			// CHECK — common in the cell schema (status/severity/enum guards) —
			// drops+recreates on every plan. Found via plan-capture, 2026-06-02.
			name:      "ANY(ARRAY[...]) list authored without comma spaces",
			natural:   "provider = ANY (ARRAY['aws','gcp','azure'])",
			canonical: "(provider = ANY (ARRAY['aws'::text, 'gcp'::text, 'azure'::text]))",
		},
		{
			// Same comma-spacing gap inside a multi-argument function call.
			name:      "multi-arg function call without comma space",
			natural:   "tenant_id = current_setting('app.tenant',true)::uuid",
			canonical: "(tenant_id = (current_setting('app.tenant', true))::uuid)",
		},
		{
			// A CHECK on a VARCHAR column: PostgreSQL casts the column to text AND
			// wraps the whole ARRAY in a "::text[]" array cast (each element becomes
			// "::character varying"). The outer "::text[]" must be stripped INCLUDING
			// its "[]" array marker — otherwise the canonical form keeps a dangling
			// "[]" and never equals the natural "outcome = ANY (ARRAY[...])", forcing
			// work_item_history's outcome_check to drop+recreate every plan. Found via
			// plan-capture (migration 6b79a43), 2026-06-02.
			name:    "varchar-column = ANY(ARRAY[...]) with outer ::text[] array cast",
			natural: "outcome = ANY (ARRAY['success','failure','partial','reassigned'])",
			canonical: "((outcome)::text = ANY ((ARRAY['success'::character varying, " +
				"'failure'::character varying, 'partial'::character varying, " +
				"'reassigned'::character varying])::text[]))",
		},
		{
			// Feature B regression: COALESCE with an empty-string literal authored in
			// natural form (no cast) must canonicalize equal to the pg_get_indexdef
			// form which adds "::character varying". Without this, the
			// blocks_cloud_id_unique mixed-expression index (which has COALESCE as one
			// of four ordered elements) would force a drop+recreate on every plan
			// because the spec writes COALESCE(account_id, '') but pg renders
			// COALESCE(account_id, ''::character varying).
			name:      "COALESCE with empty string natural equals pg ::character varying form",
			natural:   "COALESCE(account_id, '')",
			canonical: "COALESCE(account_id, ''::character varying)",
		},
		{
			// Schema-qualified enum cast in a CHECK ARRAY predicate.
			// pg_get_constraintdef returns "::global.cell_type" when the enum is
			// declared in the "global" schema. A user authors the bare cast
			// "::cell_type" (or no cast at all). Without consuming the "schema." prefix
			// in skipTypeToken the canonical form retains "global" and never equals the
			// natural form, forcing cells_dedicated_capacity_check to drop+recreate on
			// every plan.
			name:    "schema-qualified enum cast in ARRAY predicate (cells_dedicated_capacity_check)",
			natural: "(type = ANY (ARRAY['dedicated','isolated'])) AND (capacity_max = 1) OR type = 'shared'",
			canonical: "((type = ANY (ARRAY['dedicated'::global.cell_type, " +
				"'isolated'::global.cell_type])) AND (capacity_max = 1)) OR " +
				"(type = 'shared'::global.cell_type)",
		},
		{
			// Schema-qualified enum cast in a partial-index WHERE clause.
			// pg_get_indexdef returns "::global.cell_type" and "::global.cell_status"
			// for idx_cells_available_shared; a user authors bare column comparisons.
			name:      "schema-qualified enum casts in partial-index WHERE (idx_cells_available_shared)",
			natural:   "type = 'shared' AND status = 'active'",
			canonical: "(type = 'shared'::global.cell_type) AND (status = 'active'::global.cell_status)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t,
				CanonicalizeSQLExpr(test.natural),
				CanonicalizeSQLExpr(test.canonical),
				"natural and canonical forms must canonicalize equal (else churn every plan)",
			)
		})
	}
}

// Test_CanonicalizeSQLExpr_distinguishesDifferent guards against the opposite
// failure: two genuinely different expressions must NOT collapse to the same
// canonical form, or a real change would be missed and never applied.
func Test_CanonicalizeSQLExpr_distinguishesDifferent(t *testing.T) {
	assert.NotEqual(t, CanonicalizeSQLExpr("phone <> ''"), CanonicalizeSQLExpr("email <> ''"))
	assert.NotEqual(t, CanonicalizeSQLExpr("amount > 0"), CanonicalizeSQLExpr("amount >= 0"))
	assert.NotEqual(t, CanonicalizeSQLExpr("lower(email)"), CanonicalizeSQLExpr("upper(email)"))

	// A STRING literal that happens to look numeric must NOT be unquoted: PostgreSQL
	// renders "status = '5'" (text column) as "(status)::text = '5'::text", and that
	// must stay distinct from a numeric "count = 5" / "count = 5::integer". Only
	// numeric-typed cast literals are unquoted, so the text '5' keeps its quotes.
	textFive := CanonicalizeSQLExpr("(status)::text = '5'::text")
	intFive := CanonicalizeSQLExpr("count = 5")
	assert.NotEqual(t, textFive, intFive)
	assert.Equal(t, CanonicalizeSQLExpr("status = '5'"), textFive,
		"a string literal '5' must round-trip unchanged (quotes preserved)")
}
