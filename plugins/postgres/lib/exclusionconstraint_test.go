package postgres

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"

	"github.com/stretchr/testify/assert"
)

func Test_AddExclusionConstraintStatement(t *testing.T) {
	tests := []struct {
		name              string
		tableName         string
		exclusion         *schemasv1alpha4.PostgresqlTableExclusionConstraint
		expectedStatement string
	}{
		{
			name:      "unnamed single-column gist (using defaulted)",
			tableName: "rooms",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expectedStatement: `alter table rooms add constraint "rooms_room_id_excl" exclude using "gist" ("room_id" with =)`,
		},
		{
			name:      "named multi-item gist",
			tableName: "bookings",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Name:  "no_overlapping_bookings",
				Using: "gist",
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{
					{Column: "room_id", Operator: "="},
					{Column: "during", Operator: "&&"},
				},
			},
			expectedStatement: `alter table bookings add constraint "no_overlapping_bookings" exclude using "gist" ("room_id" with =, "during" with &&)`,
		},
		{
			name:      "expression item is parenthesised and emitted verbatim",
			tableName: "bookings",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Name: "no_overlap",
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{
					{Expression: "tstzrange(starts_at, ends_at)", Operator: "&&"},
				},
			},
			expectedStatement: `alter table bookings add constraint "no_overlap" exclude using "gist" ((tstzrange(starts_at, ends_at)) with &&)`,
		},
		{
			name:      "WHERE predicate is appended in parentheses",
			tableName: "rooms",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Name:  "rooms_active_excl",
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				Where: "room_id IS NOT NULL",
			},
			expectedStatement: `alter table rooms add constraint "rooms_active_excl" exclude using "gist" ("room_id" with =) where (room_id IS NOT NULL)`,
		},
		{
			name:      "storage params emitted in sorted, deterministic order",
			tableName: "rooms",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Name:  "rooms_ff_excl",
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				With:  map[string]string{"fillfactor": "70", "buffering": "off"},
			},
			expectedStatement: `alter table rooms add constraint "rooms_ff_excl" exclude using "gist" ("room_id" with =) with (buffering = off, fillfactor = 70)`,
		},
		{
			name:      "non-default access method is identifier-quoted",
			tableName: "shapes",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Name:  "shapes_geom_excl",
				Using: "spgist",
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "geom", Operator: "&&"}},
			},
			expectedStatement: `alter table shapes add constraint "shapes_geom_excl" exclude using "spgist" ("geom" with &&)`,
		},
		{
			name:      "schema-qualified table: name stays bare, relation is quoted",
			tableName: "global.bookings",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expectedStatement: `alter table "global"."bookings" add constraint "bookings_room_id_excl" exclude using "gist" ("room_id" with =)`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := AddExclusionConstraintStatement(test.tableName, test.exclusion)
			assert.Equal(t, test.expectedStatement, statement)
		})
	}
}

func Test_AddExclusionConstraintStatement_nil(t *testing.T) {
	assert.Equal(t, "", AddExclusionConstraintStatement("rooms", nil))
}

func Test_RemoveExclusionConstraintStatement(t *testing.T) {
	tests := []struct {
		name              string
		tableName         string
		exclusion         *types.ExclusionConstraint
		expectedStatement string
	}{
		{
			name:      "bare table, quoted name (always DROP CONSTRAINT, never DROP INDEX)",
			tableName: "rooms",
			exclusion: &types.ExclusionConstraint{
				Name: "rooms_room_id_excl",
			},
			expectedStatement: `alter table rooms drop constraint "rooms_room_id_excl"`,
		},
		{
			name:      "schema-qualified table, quoted name",
			tableName: "global.bookings",
			exclusion: &types.ExclusionConstraint{
				Name: "no_overlapping_bookings",
			},
			expectedStatement: `alter table "global"."bookings" drop constraint "no_overlapping_bookings"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := RemoveExclusionConstraintStatement(test.tableName, test.exclusion)
			assert.Equal(t, test.expectedStatement, statement)
		})
	}
}

func Test_RemoveExclusionConstraintStatement_nil(t *testing.T) {
	assert.Equal(t, "", RemoveExclusionConstraintStatement("rooms", nil))
}

func Test_exclusionConstraintClause(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		exclusion *schemasv1alpha4.PostgresqlTableExclusionConstraint
		expected  string
	}{
		{
			name:      "named, inline form starts with constraint keyword",
			tableName: "rooms",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Name:  "rooms_room_id_excl",
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: `constraint "rooms_room_id_excl" exclude using "gist" ("room_id" with =)`,
		},
		{
			name:      "schema-qualified table contributes only the bare table to the name",
			tableName: "global.bookings",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{
					{Expression: "tstzrange(starts_at, ends_at)", Operator: "&&"},
				},
			},
			expected: `constraint "bookings_tstzrange_starts_at_ends_at_excl" exclude using "gist" ((tstzrange(starts_at, ends_at)) with &&)`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, exclusionConstraintClause(test.tableName, test.exclusion))
		})
	}
}

// The condef strings below are real `pg_get_constraintdef(oid, true)` output, so
// parseExclusionConstraintDef is exercised against canonical reproduction. This
// is the load-bearing idempotency path: the parsed form must compare Equal to the
// desired spec so a satisfied schema re-plans to zero statements.
func Test_parseExclusionConstraintDef(t *testing.T) {
	tests := []struct {
		name     string
		condef   string
		expected types.ExclusionConstraint
	}{
		{
			name:   "single scalar element, no predicate",
			condef: "EXCLUDE USING gist (room_id WITH =)",
			expected: types.ExclusionConstraint{
				Using: "gist",
				Items: []types.ExclusionConstraintItem{{Expression: "room_id", Operator: "="}},
			},
		},
		{
			name:   "multi element with range operator",
			condef: "EXCLUDE USING gist (room_id WITH =, during WITH &&)",
			expected: types.ExclusionConstraint{
				Using: "gist",
				Items: []types.ExclusionConstraintItem{
					{Expression: "room_id", Operator: "="},
					{Expression: "during", Operator: "&&"},
				},
			},
		},
		{
			name:   "expression element with nested parens and function call",
			condef: "EXCLUDE USING gist (tstzrange(starts_at, ends_at) WITH &&)",
			expected: types.ExclusionConstraint{
				Using: "gist",
				Items: []types.ExclusionConstraintItem{
					{Expression: "tstzrange(starts_at, ends_at)", Operator: "&&"},
				},
			},
		},
		{
			name:   "partial predicate is recovered without the WHERE keyword or outer parens",
			condef: "EXCLUDE USING gist (room_id WITH =) WHERE (room_id IS NOT NULL)",
			expected: types.ExclusionConstraint{
				Using: "gist",
				Items: []types.ExclusionConstraintItem{{Expression: "room_id", Operator: "="}},
				Where: "room_id IS NOT NULL",
			},
		},
		{
			name:   "non-default access method",
			condef: "EXCLUDE USING spgist (geom WITH &&)",
			expected: types.ExclusionConstraint{
				Using: "spgist",
				Items: []types.ExclusionConstraintItem{{Expression: "geom", Operator: "&&"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := parseExclusionConstraintDef(test.condef)
			assert.Equal(t, test.expected.Using, parsed.Using)
			assert.Equal(t, test.expected.Where, parsed.Where)
			assert.Equal(t, test.expected.Items, parsed.Items)
		})
	}
}

// This proves the full round-trip that idempotency depends on: a desired spec
// (using Column) parsed back from canonical pg_get_constraintdef output must be
// reported Equal, so an already-applied EXCLUDE re-plans to nothing.
func Test_parseExclusionConstraintDef_roundtrips_with_Equals(t *testing.T) {
	desired := &schemasv1alpha4.PostgresqlTableExclusionConstraint{
		Name: "no_overlapping_bookings",
		Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{
			{Column: "room_id", Operator: "="},
			{Expression: "tstzrange(starts_at, ends_at)", Operator: "&&"},
		},
		Where: "room_id IS NOT NULL",
	}

	condef := "EXCLUDE USING gist (room_id WITH =, tstzrange(starts_at, ends_at) WITH &&) WHERE (room_id IS NOT NULL)"
	introspected := parseExclusionConstraintDef(condef)
	introspected.Name = "no_overlapping_bookings"

	assert.True(t, introspected.Equals(types.PostgresqlSchemaExclusionConstraintToExclusionConstraint(desired)),
		"introspected EXCLUDE must compare Equal to the desired spec it was created from")
}

func Test_splitItemOnWith(t *testing.T) {
	tests := []struct {
		name         string
		item         string
		wantElement  string
		wantOperator string
	}{
		{name: "plain column", item: "room_id WITH =", wantElement: "room_id ", wantOperator: " ="},
		{name: "expression element", item: "tstzrange(a, b) WITH &&", wantElement: "tstzrange(a, b) ", wantOperator: " &&"},
		{
			name: "WITH appearing inside the expression is not the separator",
			// e.g. a hypothetical function name containing 'with' must not split early.
			item:         "f_within(a) WITH =",
			wantElement:  "f_within(a) ",
			wantOperator: " =",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			element, operator := splitItemOnWith(test.item)
			assert.Equal(t, test.wantElement, element)
			assert.Equal(t, test.wantOperator, operator)
		})
	}
}
