package types

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"

	"github.com/stretchr/testify/assert"
)

func TestExclusionConstraint_Equals(t *testing.T) {
	tests := []struct {
		name      string
		exclusion *ExclusionConstraint
		other     *ExclusionConstraint
		expected  bool
	}{
		{
			name: "identical single-item gist",
			exclusion: &ExclusionConstraint{
				Name:  "rooms_room_id_excl",
				Using: "gist",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			other: &ExclusionConstraint{
				Name:  "rooms_room_id_excl",
				Using: "gist",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: true,
		},
		{
			name: "empty Using on one side defaults to gist and matches",
			exclusion: &ExclusionConstraint{
				Using: "",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			other: &ExclusionConstraint{
				Using: "gist",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: true,
		},
		{
			name: "different access method does not match",
			exclusion: &ExclusionConstraint{
				Using: "gist",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			other: &ExclusionConstraint{
				Using: "spgist",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: false,
		},
		{
			name: "differing operator does not match",
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "during", Operator: "&&"}},
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "during", Operator: "="}},
			},
			expected: false,
		},
		{
			name: "element-vs-element compares column against rendered expression",
			// The desired side sets Column; the introspected side carries the same
			// element rendered into Expression. They must compare equal.
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Expression: "room_id", Operator: "="}},
			},
			expected: true,
		},
		{
			name: "item order is significant",
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{
					{Column: "room_id", Operator: "="},
					{Column: "during", Operator: "&&"},
				},
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{
					{Column: "during", Operator: "&&"},
					{Column: "room_id", Operator: "="},
				},
			},
			expected: false,
		},
		{
			name: "different item count does not match",
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{
					{Column: "room_id", Operator: "="},
					{Column: "during", Operator: "&&"},
				},
			},
			expected: false,
		},
		{
			name: "WHERE predicate differing in case/whitespace still matches",
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				Where: "room_id IS NOT NULL",
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				Where: "room_id is not null",
			},
			expected: true,
		},
		{
			name: "different WHERE predicate does not match",
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				Where: "room_id IS NOT NULL",
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				Where: "",
			},
			expected: false,
		},
		{
			// HIGH-2 regression: a natural WHERE predicate must equal the canonical
			// text pg_get_constraintdef renders back (IS NOT NULL gains wrapping
			// parens), or the EXCLUDE constraint drops+recreates on every plan.
			name: "natural WHERE predicate equals canonical (parenthesized) form",
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				Where: "room_id is not null",
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				Where: "(room_id IS NOT NULL)",
			},
			expected: true,
		},
		{
			// HIGH-2 regression: an expression element authored in natural form must
			// equal the canonical rendered element (casts/parens added).
			name: "natural expression element equals canonical form",
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Expression: "lower(name)", Operator: "="}},
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Expression: "lower((name)::text)", Operator: "="}},
			},
			expected: true,
		},
		{
			name: "explicit names that differ do not match",
			exclusion: &ExclusionConstraint{
				Name:  "a",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			other: &ExclusionConstraint{
				Name:  "b",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: false,
		},
		{
			name: "empty name on one side is ignored (structural match wins)",
			exclusion: &ExclusionConstraint{
				Name:  "",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			other: &ExclusionConstraint{
				Name:  "rooms_room_id_excl",
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: true,
		},
		{
			name: "With storage params are NOT compared (excluded from Equals)",
			exclusion: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				With:  map[string]string{"fillfactor": "70"},
			},
			other: &ExclusionConstraint{
				Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}},
				With:  nil,
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.exclusion.Equals(test.other))
			// Equality must be symmetric.
			assert.Equal(t, test.expected, test.other.Equals(test.exclusion))
		})
	}
}

func TestExclusionConstraint_Equals_nil(t *testing.T) {
	var a *ExclusionConstraint
	var b *ExclusionConstraint
	assert.True(t, a.Equals(b))

	nonNil := &ExclusionConstraint{Items: []ExclusionConstraintItem{{Column: "room_id", Operator: "="}}}
	assert.False(t, nonNil.Equals(nil))
	assert.False(t, a.Equals(nonNil))
}

func TestGeneratePostgresqlExclusionConstraintName(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		exclusion *schemasv1alpha4.PostgresqlTableExclusionConstraint
		expected  string
	}{
		{
			name:      "explicit name is returned verbatim",
			tableName: "rooms",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Name:  "no_overlapping_bookings",
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: "no_overlapping_bookings",
		},
		{
			name:      "generated from single column",
			tableName: "rooms",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: "rooms_room_id_excl",
		},
		{
			name:      "generated from multiple items joins element fragments",
			tableName: "bookings",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{
					{Column: "room_id", Operator: "="},
					{Expression: "tstzrange(starts_at, ends_at)", Operator: "&&"},
				},
			},
			expected: "bookings_room_id_tstzrange_starts_at_ends_at_excl",
		},
		{
			name:      "schema-qualified table contributes only the bare table to the name",
			tableName: "global.bookings",
			exclusion: &schemasv1alpha4.PostgresqlTableExclusionConstraint{
				Items: []*schemasv1alpha4.PostgresqlTableExclusionConstraintItem{{Column: "room_id", Operator: "="}},
			},
			expected: "bookings_room_id_excl",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, GeneratePostgresqlExclusionConstraintName(test.tableName, test.exclusion))
		})
	}
}
