package postgres

import (
	"reflect"
	"testing"

	"github.com/schemahero/schemahero/pkg/database/types"
)

// The indexDef strings below are real `pg_get_indexdef` output captured from
// PostgreSQL 16, so the parser is exercised against canonical reproduction.
func Test_parseIndexElements(t *testing.T) {
	tests := []struct {
		name       string
		indexDef   string
		wantSorted []types.IndexColumn
		wantExpr   []string
	}{
		{
			name:       "plain single column yields nothing rich",
			indexDef:   "CREATE INDEX i ON public.users USING btree (email)",
			wantSorted: nil,
			wantExpr:   nil,
		},
		{
			name:       "plain multi column yields nothing rich",
			indexDef:   "CREATE INDEX i ON public.users USING btree (email, phone)",
			wantSorted: nil,
			wantExpr:   nil,
		},
		{
			name:       "desc ordering",
			indexDef:   "CREATE INDEX i ON public.users USING btree (created_at DESC, id)",
			wantSorted: []types.IndexColumn{{Column: "created_at", Sort: "DESC"}, {Column: "id"}},
			wantExpr:   nil,
		},
		{
			name:       "nulls first",
			indexDef:   "CREATE INDEX i ON public.users USING btree (email NULLS FIRST)",
			wantSorted: []types.IndexColumn{{Column: "email", Nulls: "FIRST"}},
			wantExpr:   nil,
		},
		{
			name:       "expression with nested parens",
			indexDef:   "CREATE INDEX i ON public.users USING btree (lower((email)::text))",
			wantSorted: nil,
			wantExpr:   []string{"lower((email)::text)"},
		},
		{
			name:       "two expressions split at top level only",
			indexDef:   "CREATE INDEX i ON public.users USING btree (lower((email)::text), upper((phone)::text))",
			wantSorted: nil,
			wantExpr:   []string{"lower((email)::text)", "upper((phone)::text)"},
		},
		{
			name:       "partial predicate is ignored by element parser",
			indexDef:   "CREATE UNIQUE INDEX i ON public.users USING btree (email) WHERE ((phone)::text <> ''::text)",
			wantSorted: nil,
			wantExpr:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSorted, gotExpr := parseIndexElements(tt.indexDef)
			if !reflect.DeepEqual(gotSorted, tt.wantSorted) {
				t.Errorf("parseIndexElements() sorted = %#v, want %#v", gotSorted, tt.wantSorted)
			}
			if !reflect.DeepEqual(gotExpr, tt.wantExpr) {
				t.Errorf("parseIndexElements() expr = %#v, want %#v", gotExpr, tt.wantExpr)
			}
		})
	}
}

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
