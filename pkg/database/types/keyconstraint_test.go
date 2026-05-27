package types

import (
	"testing"
)

func Test_KeyConstraint_GenerateName(t *testing.T) {
	tests := []struct {
		name       string
		constraint *KeyConstraint
		tableName  string
		want       string
	}{
		{
			name:       "explicit name wins",
			constraint: &KeyConstraint{Name: "my_pk", IsPrimary: true},
			tableName:  "users",
			want:       "my_pk",
		},
		{
			name:       "primary key, bare table",
			constraint: &KeyConstraint{IsPrimary: true},
			tableName:  "users",
			want:       "users_pkey",
		},
		{
			// Regression: a constraint name must not be schema-qualified, or
			// "add constraint global.users_pkey ..." is a syntax error.
			name:       "primary key, schema-qualified table",
			constraint: &KeyConstraint{IsPrimary: true},
			tableName:  "global.oauth_service_accounts",
			want:       "oauth_service_accounts_pkey",
		},
		{
			name:       "unique key, bare table",
			constraint: &KeyConstraint{Columns: []string{"email"}},
			tableName:  "users",
			want:       "users_email_key",
		},
		{
			name:       "unique key, schema-qualified table",
			constraint: &KeyConstraint{Columns: []string{"email", "tenant_id"}},
			tableName:  "global.users",
			want:       "users_email_tenant_id_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.constraint.GenerateName(tt.tableName)
			if got != tt.want {
				t.Errorf("GenerateName(%q) = %q, want %q", tt.tableName, got, tt.want)
			}
		})
	}
}
