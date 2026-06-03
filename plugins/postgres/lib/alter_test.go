package postgres

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ColumnsMatch(t *testing.T) {
	tests := []struct {
		name   string
		col1   types.Column
		col2   types.Column
		expect bool
	}{
		{
			name: "timestamp and timestamp without time zone",
			col1: types.Column{
				Name:     "a",
				DataType: "timestamp",
			},
			col2: types.Column{
				Name:     "a",
				DataType: "timestamp without time zone",
			},
			expect: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := columnsMatch(test.col1, test.col2)
			assert.Equal(t, test.expect, actual)
		})
	}
}

func Test_AlterColumnStatments(t *testing.T) {
	defaultEleven := "11"
	defaultEmpty := ""
	defaultQuotedEnum := "'pending'" // Table-spec embedded-quote convention
	defaultFunc := "gen_random_uuid()"
	defaultEnumCast := "'pending'::lifecycle_state"  // how pg stores/renders the above
	defaultActiveCast := "'active'::lifecycle_state" // a genuinely different stored default

	tests := []struct {
		name               string
		tableName          string
		desiredColumns     []*schemasv1alpha4.PostgresqlTableColumn
		existingColumn     *types.Column
		expectedStatements []string
	}{
		{
			name:      "no change",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "a",
					Type: "integer",
				},
				{
					Name: "b",
					Type: "integer",
				},
			},
			existingColumn: &types.Column{
				Name:          "b",
				DataType:      "integer",
				ColumnDefault: nil,
			},
			expectedStatements: []string{},
		},
		{
			name:      "no change varchar",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "a",
					Type: "varchar(32)",
				},
			},
			existingColumn: &types.Column{
				Name:     "a",
				DataType: "character varying (32)",
			},
			expectedStatements: []string{},
		},
		{
			name:      "change data type",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "a",
					Type: "integer",
				},
				{
					Name: "b",
					Type: "integer",
				},
			},
			existingColumn: &types.Column{
				Name:          "b",
				DataType:      "varchar(255)",
				ColumnDefault: nil,
			},
			expectedStatements: []string{`alter table "t" alter column "b" type integer`},
		},
		{
			name:      "ignore serial",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "a",
					Type: "serial",
				},
				{
					Name: "b",
					Type: "integer",
				},
			},
			existingColumn: &types.Column{
				Name:          "a",
				DataType:      "integer",
				ColumnDefault: nil,
			},
			expectedStatements: []string{},
		},
		{
			name:      "drop column",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "a",
					Type: "integer",
				},
			},
			existingColumn: &types.Column{
				Name:          "b",
				DataType:      "varchar(255)",
				ColumnDefault: nil,
			},
			expectedStatements: []string{`alter table "t" drop column "b"`},
		},
		{
			name:      "add not null constraint",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "a",
					Type: "integer",
					Constraints: &schemasv1alpha4.PostgresqlTableColumnConstraints{
						NotNull: &trueValue,
					},
				},
			},
			existingColumn: &types.Column{
				Name:          "a",
				DataType:      "integer",
				ColumnDefault: nil,
				Constraints: &types.ColumnConstraints{
					NotNull: &falseValue,
				},
			},
			expectedStatements: []string{`alter table "t" alter column "a" set not null`},
		},
		{
			name:      "drop not null constraint",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "a",
					Type: "integer",
					Constraints: &schemasv1alpha4.PostgresqlTableColumnConstraints{
						NotNull: &falseValue,
					},
				},
			},
			existingColumn: &types.Column{
				Name:          "a",
				DataType:      "integer",
				ColumnDefault: nil,
				Constraints: &types.ColumnConstraints{
					NotNull: &trueValue,
				},
			},
			expectedStatements: []string{`alter table "t" alter column "a" drop not null`},
		},
		{
			name:      "no change to not null constraint",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "t",
					Type: "text",
				},
			},
			existingColumn: &types.Column{
				Name:          "t",
				DataType:      "text",
				ColumnDefault: nil,
				Constraints: &types.ColumnConstraints{
					NotNull: &falseValue,
				},
			},
			expectedStatements: []string{},
		},
		{
			name:      "no change to not nullable timestamp using short column type",
			tableName: "ts",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "ts",
					Type: "timestamp",
					Constraints: &schemasv1alpha4.PostgresqlTableColumnConstraints{
						NotNull: &trueValue,
					},
				},
			},
			existingColumn: &types.Column{
				Name:          "ts",
				DataType:      "timestamp",
				ColumnDefault: nil,
				Constraints: &types.ColumnConstraints{
					NotNull: &trueValue,
				},
			},
			expectedStatements: []string{},
		},
		{
			name:      "no change to not nullable timestamp",
			tableName: "ts",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "ts",
					Type: "timestamp with time zone",
					Constraints: &schemasv1alpha4.PostgresqlTableColumnConstraints{
						NotNull: &trueValue,
					},
				},
			},
			existingColumn: &types.Column{
				Name:          "ts",
				DataType:      "timestamp with time zone",
				ColumnDefault: nil,
				Constraints: &types.ColumnConstraints{
					NotNull: &trueValue,
				},
			},
			expectedStatements: []string{},
		},
		{
			name:      "default set",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name:    "a",
					Type:    "integer",
					Default: &defaultEleven,
				},
			},
			existingColumn: &types.Column{
				Name:     "a",
				DataType: "integer",
			},
			expectedStatements: []string{`alter table "t" alter column "a" set default '11'`},
		},
		{
			name:      "default unset",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name: "a",
					Type: "integer",
				},
			},
			existingColumn: &types.Column{
				Name:          "a",
				DataType:      "integer",
				ColumnDefault: &defaultEleven,
			},
			expectedStatements: []string{`alter table "t" alter column "a" drop default`},
		},
		{
			name:      "default empty string",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name:    "a",
					Type:    "varchar (32)",
					Default: &defaultEmpty,
				},
			},
			existingColumn: &types.Column{
				Name:     "a",
				DataType: "character varying (32)",
			},
			expectedStatements: []string{`alter table "t" alter column "a" set default ''`},
		},
		{
			name:      "add null and default",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name:    "a",
					Type:    "varchar (32)",
					Default: &defaultEleven,
					Constraints: &schemasv1alpha4.PostgresqlTableColumnConstraints{
						NotNull: &trueValue,
					},
				},
			},
			existingColumn: &types.Column{
				Name:     "a",
				DataType: "character varying (32)",
			},
			expectedStatements: []string{
				`alter table "t" alter column "a" set default '11'`,
				`update "t" set "a"='11' where "a" is null`,
				`alter table "t" alter column "a" set not null`,
			},
		},
		{
			// Regression: an already single-quoted default must not be re-quoted
			// (was rendered as a doubled quote pair -> SQLSTATE 42601).
			name:      "quoted string default not double-quoted",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name:    "status",
					Type:    "text",
					Default: &defaultQuotedEnum,
				},
			},
			existingColumn: &types.Column{
				Name:     "status",
				DataType: "text",
			},
			expectedStatements: []string{`alter table "t" alter column "status" set default 'pending'`},
		},
		{
			// Regression: a function-call default must not be wrapped in quotes
			// (was rendered as 'gen_random_uuid()' -> SQLSTATE 22P02).
			name:      "function default not quoted",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name:    "id",
					Type:    "uuid",
					Default: &defaultFunc,
				},
			},
			existingColumn: &types.Column{
				Name:     "id",
				DataType: "uuid",
			},
			expectedStatements: []string{`alter table "t" alter column "id" set default gen_random_uuid()`},
		},
		{
			// Regression (column-default churn): a spec default in natural form
			// ('pending') must compare EQUAL to the cast form PostgreSQL stores and
			// re-renders ('pending'::lifecycle_state), so a steady-state plan emits
			// NOTHING. Without the cast-stripping compare the approver re-applies
			// SET DEFAULT every reconcile — a needless ACCESS EXCLUSIVE catalog
			// churn on a possibly-hot table.
			name:      "enum default natural form vs stored ::type cast is a no-op",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name:    "state",
					Type:    "lifecycle_state",
					Default: &defaultQuotedEnum,
				},
			},
			existingColumn: &types.Column{
				Name:          "state",
				DataType:      "lifecycle_state",
				ColumnDefault: &defaultEnumCast,
			},
			expectedStatements: []string{},
		},
		{
			// A genuinely different default (not just a cast) still re-sets.
			name:      "different default still emits set default",
			tableName: "t",
			desiredColumns: []*schemasv1alpha4.PostgresqlTableColumn{
				{
					Name:    "state",
					Type:    "lifecycle_state",
					Default: &defaultQuotedEnum, // 'pending'
				},
			},
			existingColumn: &types.Column{
				Name:          "state",
				DataType:      "lifecycle_state",
				ColumnDefault: &defaultActiveCast, // 'active'::lifecycle_state
			},
			expectedStatements: []string{`alter table "t" alter column "state" set default 'pending'`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := require.New(t)

			generatedStatements, err := AlterColumnStatements(test.tableName, []string{}, test.desiredColumns, test.existingColumn)
			req.NoError(err)
			assert.Equal(t, test.expectedStatements, generatedStatements)
		})
	}
}

func Test_columnDefaultsEqual(t *testing.T) {
	p := func(s string) *string { return &s }
	tests := []struct {
		name   string
		a      *string
		b      *string
		expect bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs value", nil, p("'pending'"), false},
		{"value vs nil", p("'pending'"), nil, false},
		{"natural vs stored enum ::type cast", p("'pending'"), p("'pending'::lifecycle_state"), true},
		{"jsonb natural vs ::jsonb cast", p("'[]'"), p("'[]'::jsonb"), true},
		{"function default paren noise", p("now()"), p("now()"), true},
		{"numeric quote-cast artifact", p("0"), p("'0'::integer"), true},
		{"genuinely different values not equal", p("'pending'"), p("'active'::lifecycle_state"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, columnDefaultsEqual(tt.a, tt.b))
		})
	}
}
