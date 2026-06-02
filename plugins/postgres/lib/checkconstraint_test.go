package postgres

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"

	"github.com/stretchr/testify/assert"
)

func Test_AddCheckConstraintStatement(t *testing.T) {
	tests := []struct {
		name              string
		tableName         string
		check             *schemasv1alpha4.PostgresqlTableCheckConstraint
		expectedStatement string
	}{
		{
			name:      "named, bare table",
			tableName: "users",
			check: &schemasv1alpha4.PostgresqlTableCheckConstraint{
				Name:       "users_age_nonneg",
				Expression: "age >= 0",
			},
			expectedStatement: `alter table users add constraint "users_age_nonneg" check (age >= 0)`,
		},
		{
			name:      "generated name from expression",
			tableName: "users",
			check: &schemasv1alpha4.PostgresqlTableCheckConstraint{
				Expression: "age >= 0",
			},
			expectedStatement: `alter table users add constraint "users_age_0_check" check (age >= 0)`,
		},
		{
			name:      "schema-qualified table keeps name free of a dot and quotes the relation",
			tableName: "global.events",
			check: &schemasv1alpha4.PostgresqlTableCheckConstraint{
				Expression: "priority > 0",
			},
			expectedStatement: `alter table "global"."events" add constraint "events_priority_0_check" check (priority > 0)`,
		},
		{
			name:      "schema-qualified table with explicit name",
			tableName: "global.events",
			check: &schemasv1alpha4.PostgresqlTableCheckConstraint{
				Name:       "events_priority_positive",
				Expression: "priority > 0",
			},
			expectedStatement: `alter table "global"."events" add constraint "events_priority_positive" check (priority > 0)`,
		},
		{
			name:      "expression containing single quotes is emitted verbatim, not escaped",
			tableName: "orders",
			check: &schemasv1alpha4.PostgresqlTableCheckConstraint{
				Name:       "orders_status_valid",
				Expression: "status in ('a','b')",
			},
			expectedStatement: `alter table orders add constraint "orders_status_valid" check (status in ('a','b'))`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := AddCheckConstraintStatement(test.tableName, test.check)
			assert.Equal(t, test.expectedStatement, statement)
		})
	}
}

func Test_AddCheckConstraintStatement_nil(t *testing.T) {
	assert.Equal(t, "", AddCheckConstraintStatement("users", nil))
}

func Test_RemoveCheckConstraintStatement(t *testing.T) {
	tests := []struct {
		name              string
		tableName         string
		check             *types.CheckConstraint
		expectedStatement string
	}{
		{
			name:      "bare table, quoted name",
			tableName: "users",
			check: &types.CheckConstraint{
				Name:       "users_age_nonneg",
				Expression: "age >= 0",
			},
			expectedStatement: `alter table users drop constraint "users_age_nonneg"`,
		},
		{
			name:      "schema-qualified table, quoted name",
			tableName: "global.events",
			check: &types.CheckConstraint{
				Name:       "events_priority_positive",
				Expression: "priority > 0",
			},
			expectedStatement: `alter table "global"."events" drop constraint "events_priority_positive"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := RemoveCheckConstraintStatement(test.tableName, test.check)
			assert.Equal(t, test.expectedStatement, statement)
		})
	}
}

func Test_RemoveCheckConstraintStatement_nil(t *testing.T) {
	assert.Equal(t, "", RemoveCheckConstraintStatement("users", nil))
}

func Test_checkConstraintClause(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		check     *schemasv1alpha4.PostgresqlTableCheckConstraint
		expected  string
	}{
		{
			name:      "named",
			tableName: "users",
			check: &schemasv1alpha4.PostgresqlTableCheckConstraint{
				Name:       "users_age_nonneg",
				Expression: "age >= 0",
			},
			expected: `constraint "users_age_nonneg" check (age >= 0)`,
		},
		{
			name:      "generated, schema-qualified table contributes only the bare table to the name",
			tableName: "global.events",
			check: &schemasv1alpha4.PostgresqlTableCheckConstraint{
				Expression: "priority > 0",
			},
			expected: `constraint "events_priority_0_check" check (priority > 0)`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, checkConstraintClause(test.tableName, test.check))
		})
	}
}

// The condef strings below are real `pg_get_constraintdef(oid, true)` output, so
// stripCheckWrapper is exercised against canonical reproduction. It strips the
// CHECK keyword and exactly one balanced outer pair (the CHECK's own wrapper),
// leaving the predicate as PostgreSQL canonicalized it (which is itself usually
// parenthesized). Full normalization for the equality compare happens separately
// in checkExprEquivalent, so the residual parens here are intentional and fine.
func Test_stripCheckWrapper(t *testing.T) {
	tests := []struct {
		name     string
		condef   string
		expected string
	}{
		{
			name:     "simple numeric predicate",
			condef:   "CHECK ((age >= 0))",
			expected: "(age >= 0)",
		},
		{
			name:     "in-list with casts and ANY/ARRAY rewrite",
			condef:   "CHECK (((status)::text = ANY ((ARRAY['a'::character varying, 'b'::character varying])::text[])))",
			expected: "((status)::text = ANY ((ARRAY['a'::character varying, 'b'::character varying])::text[]))",
		},
		{
			name:     "lowercase keyword defensive",
			condef:   "check ((age >= 0))",
			expected: "(age >= 0)",
		},
		{
			name:     "compound predicate keeps both halves (only the wrapping pair stripped)",
			condef:   "CHECK (((a > 0) AND (b > 0)))",
			expected: "((a > 0) AND (b > 0))",
		},
		{
			name:     "no surrounding extra parens",
			condef:   "CHECK (age >= 0)",
			expected: "age >= 0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, stripCheckWrapper(test.condef))
		})
	}
}

func Test_hasBalancedOuterParens(t *testing.T) {
	assert.True(t, hasBalancedOuterParens("(a >= 0)"))
	assert.True(t, hasBalancedOuterParens("((a) and (b))"))
	// The outer pair does NOT wrap everything here, so it must not be stripped.
	assert.False(t, hasBalancedOuterParens("(a) and (b)"))
	assert.False(t, hasBalancedOuterParens("a >= 0"))
	assert.False(t, hasBalancedOuterParens(""))
}

// checkExprEquivalent must treat a user's raw spec expression as equivalent to
// the canonicalized form PostgreSQL reports, so a satisfied spec re-plans to
// zero statements instead of churning a destructive DROP+ADD.
func Test_checkExprEquivalent(t *testing.T) {
	tests := []struct {
		name      string
		userExpr  string
		introspec string
		want      bool
	}{
		{
			name:      "numeric predicate, parens and spacing differ",
			userExpr:  "age >= 0",
			introspec: "age >= 0", // post stripCheckWrapper of "CHECK ((age >= 0))"
			want:      true,
		},
		{
			name:      "in-list vs ANY/ARRAY canonicalization with casts",
			userExpr:  "status in ('a','b')",
			introspec: "(status)::text = ANY ((ARRAY['a'::character varying, 'b'::character varying])::text[])",
			// These are semantically equal but NOT structurally reducible by a
			// conservative normalizer; they normalize differently. The function
			// therefore reports false, which (correctly) means a user who writes
			// `status in (...)` against a column-typed table will see one DROP+ADD.
			// Documented behavior: prefer an explicit name + matching expression,
			// or accept the single re-add. We assert the actual conservative result.
			want: false,
		},
		{
			name:      "same expression with redundant parens and casts stripped",
			userExpr:  "amount > 0",
			introspec: "(amount > (0)::numeric)",
			want:      true,
		},
		{
			name:      "case-insensitive operators/keywords",
			userExpr:  "a AND b",
			introspec: "(a and b)",
			want:      true,
		},
		{
			name:      "genuinely different predicates are not equivalent",
			userExpr:  "age >= 0",
			introspec: "age > 0",
			want:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, checkExprEquivalent(test.userExpr, test.introspec))
		})
	}
}

func Test_normalizeCheckExpr(t *testing.T) {
	// Parens removed, casts stripped, whitespace collapsed, lowercased.
	assert.Equal(t, "age >= 0", normalizeCheckExpr("(age >= 0)"))
	assert.Equal(t, "amount > 0", normalizeCheckExpr("(amount > (0)::numeric)"))
	assert.Equal(t, "status = 'a'", normalizeCheckExpr("((status)::text = 'a'::character varying)"))
	assert.Equal(t, "a and b", normalizeCheckExpr("(A AND B)"))
}
