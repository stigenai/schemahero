package types

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"

	"github.com/stretchr/testify/assert"
)

func sp(s string) *string { return &s }

func Test_PostgresqlSchemaPolicyToPolicy_normalizesDefaults(t *testing.T) {
	// Empty command/permissive/roles must normalize to ALL / PERMISSIVE / ["public"]
	// so a minimal spec compares equal to what pg_policy returns.
	got := PostgresqlSchemaPolicyToPolicy(&schemasv1alpha4.PostgresqlTablePolicy{
		Name: "p",
	})
	assert.Equal(t, "ALL", got.Command)
	assert.True(t, got.Permissive)
	assert.Equal(t, []string{"public"}, got.Roles)
	assert.Nil(t, got.Using)
	assert.Nil(t, got.WithCheck)

	// RESTRICTIVE + explicit command/roles round-trip.
	got2 := PostgresqlSchemaPolicyToPolicy(&schemasv1alpha4.PostgresqlTablePolicy{
		Name:       "p",
		Command:    "select",
		Permissive: "RESTRICTIVE",
		Roles:      []string{"b", "a"},
		Using:      sp("x = 1"),
	})
	assert.Equal(t, "SELECT", got2.Command)
	assert.False(t, got2.Permissive)
	assert.Equal(t, []string{"a", "b"}, got2.Roles) // sorted
	assert.Equal(t, "x = 1", *got2.Using)
}

func Test_Policy_Equals(t *testing.T) {
	tests := []struct {
		name string
		a    *Policy
		b    *Policy
		want bool
	}{
		{
			name: "identical",
			a:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"public"}, Using: sp("x = 1")},
			b:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"public"}, Using: sp("x = 1")},
			want: true,
		},
		{
			name: "expr whitespace/case normalized equal",
			a:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"public"}, Using: sp("X  =   1")},
			b:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"public"}, Using: sp("x = 1")},
			want: true,
		},
		{
			name: "default command vs empty command equal",
			a:    &Policy{Name: "p", Command: "", Permissive: true, Roles: []string{"public"}},
			b:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"public"}},
			want: true,
		},
		{
			name: "empty roles equals public",
			a:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: nil},
			b:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"public"}},
			want: true,
		},
		{
			name: "roles compared as set",
			a:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"a", "b"}},
			b:    &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"b", "a"}},
			want: true,
		},
		{
			name: "different name",
			a:    &Policy{Name: "p", Command: "ALL", Permissive: true},
			b:    &Policy{Name: "q", Command: "ALL", Permissive: true},
			want: false,
		},
		{
			name: "different command",
			a:    &Policy{Name: "p", Command: "SELECT", Permissive: true},
			b:    &Policy{Name: "p", Command: "INSERT", Permissive: true},
			want: false,
		},
		{
			name: "different permissive",
			a:    &Policy{Name: "p", Command: "ALL", Permissive: true},
			b:    &Policy{Name: "p", Command: "ALL", Permissive: false},
			want: false,
		},
		{
			name: "different expr",
			a:    &Policy{Name: "p", Command: "ALL", Permissive: true, Using: sp("x = 1")},
			b:    &Policy{Name: "p", Command: "ALL", Permissive: true, Using: sp("x = 2")},
			want: false,
		},
		{
			name: "nil using vs set using differ",
			a:    &Policy{Name: "p", Command: "ALL", Permissive: true},
			b:    &Policy{Name: "p", Command: "ALL", Permissive: true, Using: sp("x = 1")},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.a.Equals(test.b))
		})
	}
}

func Test_Policy_AttributesEqual_ignoresExpression(t *testing.T) {
	// AttributesEqual is the recreate-decision comparator: it must IGNORE the
	// USING/WITH CHECK expressions (which pg_get_expr re-renders canonically) so an
	// expression-only difference does NOT trigger a destructive drop+recreate.
	a := &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"public"}, Using: sp("x = 1")}
	b := &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"public"}, Using: sp("(x = (1)::integer)")}
	assert.True(t, a.AttributesEqual(b), "expression difference must not change attribute equality")

	// But a real attribute difference (roles) must be detected.
	c := &Policy{Name: "p", Command: "ALL", Permissive: true, Roles: []string{"app_user"}, Using: sp("x = 1")}
	assert.False(t, a.AttributesEqual(c))
}

func Test_PolicyCommandFromCatalogChar(t *testing.T) {
	cases := map[string]string{
		"*": "ALL",
		"r": "SELECT",
		"a": "INSERT",
		"w": "UPDATE",
		"d": "DELETE",
		"?": "ALL", // unknown falls back to ALL
	}
	for char, want := range cases {
		assert.Equal(t, want, PolicyCommandFromCatalogChar(char), "polcmd %q", char)
	}
}
