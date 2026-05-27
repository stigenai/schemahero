package postgres

import "testing"

func Test_sanitizeTableName(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		want      string
	}{
		{name: "bare name is quoted", tableName: "users", want: `"users"`},
		// Regression: a schema-qualified name must become "schema"."table", not
		// the single quoted identifier "schema.table" (which lands a dotted-name
		// table in the public schema).
		{name: "qualified name splits", tableName: "global.oauth_service_accounts", want: `"global"."oauth_service_accounts"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeTableName(tt.tableName); got != tt.want {
				t.Errorf("sanitizeTableName(%q) = %q, want %q", tt.tableName, got, tt.want)
			}
		})
	}
}

func Test_qualifyTableName(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		want      string
	}{
		// Bare names pass through unquoted, preserving the prior raw rendering.
		{name: "bare name passes through", tableName: "users", want: "users"},
		{name: "qualified name splits", tableName: "global.oauth_service_accounts", want: `"global"."oauth_service_accounts"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qualifyTableName(tt.tableName); got != tt.want {
				t.Errorf("qualifyTableName(%q) = %q, want %q", tt.tableName, got, tt.want)
			}
		})
	}
}
