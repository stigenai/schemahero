package types

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func Test_GenerateMysqlIndexName(t *testing.T) {
	tests := []struct {
		name        string
		tableName   string
		schemaIndex *schemasv1alpha4.MysqlTableIndex
		want        string
	}{
		{
			name:      "short index",
			tableName: "table_name",
			schemaIndex: &schemasv1alpha4.MysqlTableIndex{
				Columns: []string{
					"col1",
					"col2",
				},
			},
			want: "idx_table_name_col1_col2",
		},
		{
			name:      "long index",
			tableName: "very_very_very_long_table_name",
			schemaIndex: &schemasv1alpha4.MysqlTableIndex{
				Columns: []string{
					"collumn_1",
					"collumn_2",
					"collumn_3",
					"collumn_4",
					"collumn_5",
					"collumn_6",
					"collumn_7",
				},
			},
			want: "idx_very_very_very_long_table_name_collumn_1_collumn_2_collumn_3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateMysqlIndexName(tt.tableName, tt.schemaIndex); got != tt.want {
				t.Errorf("GenerateMysqlIndexName() = %s, want %s", got, tt.want)
			}
		})
	}
}

func Test_IndexEquals(t *testing.T) {
	tests := []struct {
		name string
		a    *Index
		b    *Index
		want bool
	}{
		{
			name: "identical plain indexes (legacy path)",
			a:    &Index{Name: "i", Columns: []string{"a", "b"}},
			b:    &Index{Name: "i", Columns: []string{"a", "b"}},
			want: true,
		},
		{
			name: "legacy columns are order-insensitive",
			a:    &Index{Name: "i", Columns: []string{"a", "b"}},
			b:    &Index{Name: "i", Columns: []string{"b", "a"}},
			want: true,
		},
		{
			name: "different name",
			a:    &Index{Name: "i", Columns: []string{"a"}},
			b:    &Index{Name: "j", Columns: []string{"a"}},
			want: false,
		},
		{
			name: "empty type equals explicit btree (no churn)",
			a:    &Index{Name: "i", Columns: []string{"a"}, Type: ""},
			b:    &Index{Name: "i", Columns: []string{"a"}, Type: "btree"},
			want: true,
		},
		{
			name: "empty type equals upper-case BTREE",
			a:    &Index{Name: "i", Columns: []string{"a"}, Type: ""},
			b:    &Index{Name: "i", Columns: []string{"a"}, Type: "BTREE"},
			want: true,
		},
		{
			name: "gin differs from btree",
			a:    &Index{Name: "i", Columns: []string{"a"}, Type: "gin"},
			b:    &Index{Name: "i", Columns: []string{"a"}, Type: ""},
			want: false,
		},
		{
			name: "same method gin equal",
			a:    &Index{Name: "i", Columns: []string{"a"}, Type: "gin"},
			b:    &Index{Name: "i", Columns: []string{"a"}, Type: "GIN"},
			want: true,
		},
		{
			name: "where predicate whitespace/case insensitive",
			a:    &Index{Name: "i", Columns: []string{"a"}, Where: "phone <> ''"},
			b:    &Index{Name: "i", Columns: []string{"a"}, Where: "PHONE   <>   ''"},
			want: true,
		},
		{
			name: "different where predicate",
			a:    &Index{Name: "i", Columns: []string{"a"}, Where: "a is null"},
			b:    &Index{Name: "i", Columns: []string{"a"}, Where: "a is not null"},
			want: false,
		},
		{
			// HIGH-2 regression: a partial-index predicate authored in NATURAL form
			// must equal the CANONICAL text pg_get_expr renders back, or the index
			// drops+recreates on every plan.
			name: "natural where predicate equals canonical (pg_get_expr) form",
			a:    &Index{Name: "i", Columns: []string{"a"}, Where: "phone <> ''"},
			b:    &Index{Name: "i", Columns: []string{"a"}, Where: "((phone)::text <> ''::text)"},
			want: true,
		},
		{
			name: "empty where equals empty where",
			a:    &Index{Name: "i", Columns: []string{"a"}},
			b:    &Index{Name: "i", Columns: []string{"a"}, Where: ""},
			want: true,
		},
		{
			name: "sorted columns equal positionally",
			a:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a", Sort: "DESC"}, {Column: "b"}}},
			b:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a", Sort: "DESC"}, {Column: "b"}}},
			want: true,
		},
		{
			name: "omitted sort equals explicit ASC",
			a:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a"}}},
			b:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a", Sort: "ASC"}}},
			want: true,
		},
		{
			name: "sorted columns order is significant",
			a:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a"}, {Column: "b"}}},
			b:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "b"}, {Column: "a"}}},
			want: false,
		},
		{
			name: "different sort direction differs",
			a:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a", Sort: "DESC"}}},
			b:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a", Sort: "ASC"}}},
			want: false,
		},
		{
			name: "expressions equal after case + whitespace normalization",
			a:    &Index{Name: "i", Expressions: []string{"coalesce(a, b)"}},
			b:    &Index{Name: "i", Expressions: []string{"COALESCE(a,   b)"}},
			want: true,
		},
		{
			name: "different expressions differ",
			a:    &Index{Name: "i", Expressions: []string{"lower(email)"}},
			b:    &Index{Name: "i", Expressions: []string{"upper(email)"}},
			want: false,
		},
		{
			// HIGH-2 regression: a functional-index expression authored in NATURAL
			// form must equal the CANONICAL text pg_get_indexdef renders back.
			name: "natural expression equals canonical (pg_get_indexdef) form",
			a:    &Index{Name: "i", Expressions: []string{"lower(email)"}},
			b:    &Index{Name: "i", Expressions: []string{"lower((email)::text)"}},
			want: true,
		},
		{
			name: "one side has sorted columns, other plain columns: not equal",
			a:    &Index{Name: "i", Columns: []string{"a"}},
			b:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a", Sort: "DESC"}}},
			want: false,
		},
		// === Feature A: operator class (opClass) equality ===
		{
			// TRACE: spec WITH opclass must equal introspected WITH same opclass
			// (steady-state no-op for a GIN index with jsonb_path_ops).
			name: "opclass jsonb_path_ops on both sides: equal (no churn)",
			a: &Index{
				Name: "idx_blocks_conditions_gin",
				Type: "gin",
				SortedColumns: []IndexColumn{
					{Column: "conditions", OpClass: "jsonb_path_ops"},
				},
			},
			b: &Index{
				Name: "idx_blocks_conditions_gin",
				Type: "gin",
				SortedColumns: []IndexColumn{
					{Column: "conditions", OpClass: "jsonb_path_ops"},
				},
			},
			want: true,
		},
		{
			// TRACE: spec WITHOUT opclass vs introspected WITH opclass must be
			// unequal — they are genuinely different indexes and must trigger a
			// drop+recreate rather than silently ignoring the opclass mismatch.
			name: "spec without opclass vs introspected with opclass: not equal",
			a: &Index{
				Name: "idx_blocks_conditions_gin",
				Type: "gin",
				SortedColumns: []IndexColumn{
					{Column: "conditions"},
				},
			},
			b: &Index{
				Name: "idx_blocks_conditions_gin",
				Type: "gin",
				SortedColumns: []IndexColumn{
					{Column: "conditions", OpClass: "jsonb_path_ops"},
				},
			},
			want: false,
		},
		{
			// opclass comparison is case-insensitive (pg renders it lowercase;
			// a spec author may use mixed case).
			name: "opclass comparison is case-insensitive",
			a: &Index{
				Name: "i",
				SortedColumns: []IndexColumn{
					{Column: "name", OpClass: "TEXT_PATTERN_OPS"},
				},
			},
			b: &Index{
				Name: "i",
				SortedColumns: []IndexColumn{
					{Column: "name", OpClass: "text_pattern_ops"},
				},
			},
			want: true,
		},
		// === Feature B: mixed column + expression ordered list equality ===
		{
			// TRACE: the blocks_cloud_id_unique index as introspected (all elements
			// as Expressions, in positional order) must equal the spec authored with
			// natural COALESCE form and no ::character varying cast, with the
			// partial-index WHERE predicate canonicalizing equal.
			name: "mixed ordered expression list equals spec with natural COALESCE form",
			a: &Index{
				Name:     "blocks_cloud_id_unique",
				IsUnique: true,
				Expressions: []string{
					"provider",
					"resource_type",
					"COALESCE(account_id, ''::character varying)",
					"cloud_id",
				},
				Where: "(cloud_id IS NOT NULL) AND (state <> ALL (ARRAY['deleted'::lifecycle_state, 'soft_deleted'::lifecycle_state]))",
			},
			b: &Index{
				Name:     "blocks_cloud_id_unique",
				IsUnique: true,
				Expressions: []string{
					"provider",
					"resource_type",
					"COALESCE(account_id, '')",
					"cloud_id",
				},
				Where: "(cloud_id IS NOT NULL) AND (state <> ALL (ARRAY['deleted'::lifecycle_state, 'soft_deleted'::lifecycle_state]))",
			},
			want: true,
		},
		{
			// Positional order in the mixed expression list is significant — a
			// reordering of columns must not compare equal.
			name: "mixed expression list with different order: not equal",
			a: &Index{
				Name:        "i",
				Expressions: []string{"provider", "resource_type", "COALESCE(account_id, '')", "cloud_id"},
			},
			b: &Index{
				Name:        "i",
				Expressions: []string{"resource_type", "provider", "COALESCE(account_id, '')", "cloud_id"},
			},
			want: false,
		},
		{
			// Schema-qualified enum cast in a partial-index WHERE clause.
			// pg_get_indexdef returns "::global.cell_type" and "::global.cell_status"
			// for idx_cells_available_shared. A user authors the predicate with bare
			// string literals (no cast); the planner must treat these as equal so the
			// index is not dropped+recreated on every plan.
			name: "schema-qualified enum casts in WHERE equal bare string literals (idx_cells_available_shared)",
			a: &Index{
				Name:    "idx_cells_available_shared",
				Columns: []string{"tenant_id"},
				Where:   "type = 'shared' AND status = 'active'",
			},
			b: &Index{
				Name:    "idx_cells_available_shared",
				Columns: []string{"tenant_id"},
				Where:   "(type = 'shared'::global.cell_type) AND (status = 'active'::global.cell_status)",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equals(tt.b); got != tt.want {
				t.Errorf("Equals() = %v, want %v", got, tt.want)
			}
			// Equals must be symmetric.
			if got := tt.b.Equals(tt.a); got != tt.want {
				t.Errorf("Equals() reversed = %v, want %v", got, tt.want)
			}
		})
	}
}
