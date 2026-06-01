package postgres

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"

	"github.com/stretchr/testify/assert"
)

func Test_AddIndexStatement(t *testing.T) {
	tests := []struct {
		name              string
		tableName         string
		schemaIndex       *schemasv1alpha4.PostgresqlTableIndex
		expectedStatement string
	}{
		{
			name:      "no name, one column, not specified unique",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{
					"c1",
				},
			},
			expectedStatement: `create index idx_t2_c1 on t2 (c1)`,
		},
		{
			name:      "specified name, one column, not specified unique",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{
					"c1",
				},
				Name: "idx_name",
			},
			expectedStatement: `create index idx_name on t2 (c1)`,
		},
		{
			name:      "no name, two columns, not specified unique",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{
					"c1",
					"c2",
				},
			},
			expectedStatement: `create index idx_t2_c1_c2 on t2 (c1, c2)`,
		},
		{
			name:      "np name, one column, unique",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{
					"c1",
				},
				IsUnique: true,
			},
			expectedStatement: `create unique index idx_t2_c1 on t2 (c1)`,
		},
		{
			name:      "with fillfactor option",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{
					"c1",
				},
				With: map[string]string{
					"fillfactor": "70",
				},
			},
			expectedStatement: `create index idx_t2_c1 on t2 (c1) with (fillfactor = 70)`,
		},
		{
			name:      "with multiple options",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{
					"c1",
					"c2",
				},
				Name: "idx_custom",
				With: map[string]string{
					"fillfactor":             "80",
					"gin_pending_list_limit": "64",
				},
			},
			expectedStatement: `create index idx_custom on t2 (c1, c2) with (fillfactor = 80, gin_pending_list_limit = 64)`,
		},
		{
			name:      "unique index with with clause",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{
					"c1",
				},
				IsUnique: true,
				With: map[string]string{
					"fillfactor": "90",
				},
			},
			expectedStatement: `create unique index idx_t2_c1 on t2 (c1) with (fillfactor = 90)`,
		},
		{
			name:      "index method hash",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{"c1"},
				Name:    "idx_hash",
				Type:    "hash",
			},
			expectedStatement: `create index idx_hash on t2 using "hash" (c1)`,
		},
		{
			name:      "index method gin",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{"data"},
				Name:    "idx_gin",
				Type:    "gin",
			},
			expectedStatement: `create index idx_gin on t2 using "gin" (data)`,
		},
		{
			name:      "btree type is the default and omits using",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns: []string{"c1"},
				Name:    "idx_btree",
				Type:    "btree",
			},
			expectedStatement: `create index idx_btree on t2 (c1)`,
		},
		{
			name:      "partial index with where predicate",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Columns:  []string{"email"},
				Name:     "idx_partial",
				IsUnique: true,
				Where:    "phone <> ''",
			},
			expectedStatement: `create unique index idx_partial on t2 (email) where phone <> ''`,
		},
		{
			name:      "expression index",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Name:        "idx_lower_email",
				Expressions: []string{"lower(email)"},
			},
			// Expression elements are parenthesised per the index grammar, so the
			// element list "(...)" wraps the already-parenthesised expression.
			expectedStatement: `create index idx_lower_email on t2 ((lower(email)))`,
		},
		{
			name:      "sorted columns asc/desc and nulls",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Name: "idx_sorted",
				SortedColumns: []*schemasv1alpha4.PostgresqlTableIndexColumn{
					{Column: "created_at", Sort: "DESC"},
					{Column: "id"},
					{Column: "name", Nulls: "LAST"},
				},
			},
			expectedStatement: `create index idx_sorted on t2 (created_at desc, id, name nulls last)`,
		},
		{
			name:      "method, expression, with, and where combined",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Name:        "idx_combo",
				Type:        "gin",
				Expressions: []string{"to_tsvector('english', body)"},
				With:        map[string]string{"fastupdate": "off"},
				Where:       "deleted_at is null",
			},
			expectedStatement: `create index idx_combo on t2 using "gin" ((to_tsvector('english', body))) with (fastupdate = off) where deleted_at is null`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addIndexStatement := AddIndexStatement(test.tableName, test.schemaIndex)

			assert.Equal(t, test.expectedStatement, addIndexStatement)
		})
	}
}
