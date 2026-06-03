package postgres

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"

	"github.com/stretchr/testify/assert"
)

// Test_RemoveIndexStatement_alwaysIfExists is the MEDIUM-1 regression: a DROP
// INDEX must ALWAYS carry IF EXISTS, for BOTH unique and non-unique indexes, so a
// re-run / partial prior apply / concurrent reconcile cannot wedge the plan with a
// 42704 "index does not exist". Previously only the unique branch was guarded.
func Test_RemoveIndexStatement_alwaysIfExists(t *testing.T) {
	unique := RemoveIndexStatement("users", &types.Index{Name: "idx_users_email", IsUnique: true})
	assert.Equal(t, `drop index if exists "idx_users_email"`, unique)

	nonUnique := RemoveIndexStatement("users", &types.Index{Name: "idx_users_created", IsUnique: false})
	assert.Equal(t, `drop index if exists "idx_users_created"`, nonUnique)
}

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
		// === Feature A: operator class in SortedColumn ===
		{
			// TRACE: a GIN index with jsonb_path_ops must render the opclass as a
			// bare lowercase token after the column and before any sort/nulls —
			// matching pg_get_indexdef output so that plan equality holds on
			// round-trip.
			name:      "gin index with jsonb_path_ops opclass",
			tableName: "blocks",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Name: "idx_blocks_conditions_gin",
				Type: "gin",
				SortedColumns: []*schemasv1alpha4.PostgresqlTableIndexColumn{
					{Column: "conditions", OpClass: "jsonb_path_ops"},
				},
			},
			expectedStatement: `create index idx_blocks_conditions_gin on blocks using "gin" (conditions jsonb_path_ops)`,
		},
		{
			// opclass with sort direction: opclass must appear between the column
			// and the DESC/NULLS tokens (grammar: col opclass desc nulls last).
			name:      "opclass with sort direction",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Name: "idx_data",
				Type: "gin",
				SortedColumns: []*schemasv1alpha4.PostgresqlTableIndexColumn{
					{Column: "data", OpClass: "gin_trgm_ops", Sort: "DESC", Nulls: "LAST"},
				},
			},
			expectedStatement: `create index idx_data on t2 using "gin" (data gin_trgm_ops desc nulls last)`,
		},
		{
			// No opclass: SortedColumn without OpClass must render unchanged from
			// the pre-feature behaviour (opclass omitted entirely).
			name:      "sorted column without opclass is unchanged",
			tableName: "t2",
			schemaIndex: &schemasv1alpha4.PostgresqlTableIndex{
				Name: "idx_sorted_no_opclass",
				SortedColumns: []*schemasv1alpha4.PostgresqlTableIndexColumn{
					{Column: "created_at", Sort: "DESC"},
					{Column: "id"},
				},
			},
			expectedStatement: `create index idx_sorted_no_opclass on t2 (created_at desc, id)`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addIndexStatement := AddIndexStatement(test.tableName, test.schemaIndex)

			assert.Equal(t, test.expectedStatement, addIndexStatement)
		})
	}
}
