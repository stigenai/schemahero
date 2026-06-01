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
			name: "one side has sorted columns, other plain columns: not equal",
			a:    &Index{Name: "i", Columns: []string{"a"}},
			b:    &Index{Name: "i", SortedColumns: []IndexColumn{{Column: "a", Sort: "DESC"}}},
			want: false,
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
