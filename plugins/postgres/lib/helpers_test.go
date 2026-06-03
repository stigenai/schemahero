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
		// === Feature A: operator class (opClass) recovery ===
		{
			// TRACE: introspecting "USING gin (conditions jsonb_path_ops)" must
			// recover OpClass="jsonb_path_ops" on the SortedColumn for "conditions".
			// pg_get_indexdef renders the opclass as a bare lowercase token after the
			// column and before any sort/nulls keywords.
			name:     "gin index with non-default opclass",
			indexDef: "CREATE INDEX idx_blocks_conditions_gin ON public.blocks USING gin (conditions jsonb_path_ops)",
			wantSorted: []types.IndexColumn{
				{Column: "conditions", OpClass: "jsonb_path_ops"},
			},
			wantExpr: nil,
		},
		{
			// opclass followed by explicit sort direction: opclass comes before
			// DESC/ASC in pg_get_indexdef output. This exercises the correct
			// token ordering in parseSortedColumnElement.
			name:     "opclass with sort direction",
			indexDef: "CREATE INDEX i ON t USING gin (data gin_trgm_ops DESC NULLS LAST)",
			wantSorted: []types.IndexColumn{
				{Column: "data", OpClass: "gin_trgm_ops", Sort: "DESC", Nulls: "LAST"},
			},
			wantExpr: nil,
		},
		{
			// No opclass (default btree): the existing sort parsing must be
			// unaffected — DESC/NULLS tokens are still keywords, not an opclass.
			name:     "sort keywords are not mistaken for opclass",
			indexDef: "CREATE INDEX i ON t USING btree (created_at DESC NULLS FIRST)",
			wantSorted: []types.IndexColumn{
				{Column: "created_at", Sort: "DESC", Nulls: "FIRST"},
			},
			wantExpr: nil,
		},
		// === Feature B: mixed column + expression ordered list ===
		{
			// TRACE: the blocks_cloud_id_unique index introspection — a mix of plain
			// column references and a COALESCE expression. The element list must be
			// returned entirely as Expressions in positional order, with
			// SortedColumns=nil, to preserve the ordering that a split structure
			// cannot represent.
			name:       "mixed plain columns and expression returns all as ordered expressions",
			indexDef:   "CREATE UNIQUE INDEX blocks_cloud_id_unique ON public.blocks USING btree (provider, resource_type, COALESCE(account_id, ''::character varying), cloud_id) WHERE ((cloud_id IS NOT NULL) AND (state <> ALL (ARRAY['deleted'::lifecycle_state, 'soft_deleted'::lifecycle_state])))",
			wantSorted: nil,
			wantExpr: []string{
				"provider",
				"resource_type",
				"COALESCE(account_id, ''::character varying)",
				"cloud_id",
			},
		},
		{
			// A purely plain multi-column index (no expressions) must still use the
			// SortedColumns path when it carries ordering — the mixed-expression
			// guard must not activate for the pure ordering case.
			name:     "pure ordered columns are not affected by mixed-expression guard",
			indexDef: "CREATE INDEX i ON public.blocks USING btree (created_at DESC, id)",
			wantSorted: []types.IndexColumn{
				{Column: "created_at", Sort: "DESC"},
				{Column: "id"},
			},
			wantExpr: nil,
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
