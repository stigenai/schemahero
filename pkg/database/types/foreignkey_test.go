package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestForeignKey_Equals(t *testing.T) {
	tests := []struct {
		name     string
		fk       *ForeignKey
		other    *ForeignKey
		expected bool
	}{
		{
			name: "identical foreign keys",
			fk: &ForeignKey{
				Name:          "assignment_employee_id_fkey",
				ChildColumns:  []string{"employee_id"},
				ParentTable:   "employee",
				ParentColumns: []string{"id"},
				OnDelete:      "CASCADE",
			},
			other: &ForeignKey{
				Name:          "assignment_employee_id_fkey",
				ChildColumns:  []string{"employee_id"},
				ParentTable:   "employee",
				ParentColumns: []string{"id"},
				OnDelete:      "CASCADE",
			},
			expected: true,
		},
		{
			name: "different on delete action",
			fk: &ForeignKey{
				Name:          "fk1",
				ChildColumns:  []string{"col1"},
				ParentTable:   "parent",
				ParentColumns: []string{"id"},
				OnDelete:      "CASCADE",
			},
			other: &ForeignKey{
				Name:          "fk1",
				ChildColumns:  []string{"col1"},
				ParentTable:   "parent",
				ParentColumns: []string{"id"},
				OnDelete:      "SET NULL",
			},
			expected: false,
		},
		{
			// This is the key bug scenario: the DB returns the auto-generated
			// constraint name and uppercase OnDelete, but the spec has no
			// explicit name and lowercase onDelete. These should be considered
			// equal because they represent the same foreign key.
			name: "db has name and uppercase CASCADE, spec has no name and lowercase cascade",
			fk: &ForeignKey{
				Name:          "assignment_employee_id_fkey",
				ChildColumns:  []string{"employee_id"},
				ParentTable:   "employee",
				ParentColumns: []string{"id"},
				OnDelete:      "CASCADE",
			},
			other: &ForeignKey{
				Name:          "",
				ChildColumns:  []string{"employee_id"},
				ParentTable:   "employee",
				ParentColumns: []string{"id"},
				OnDelete:      "cascade",
			},
			expected: true,
		},
		{
			// DB returns "NO ACTION" as the default when no ON DELETE is specified
			name: "db has NO ACTION, spec has empty onDelete",
			fk: &ForeignKey{
				Name:          "assignment_employee_id_fkey",
				ChildColumns:  []string{"employee_id"},
				ParentTable:   "employee",
				ParentColumns: []string{"id"},
				OnDelete:      "NO ACTION",
			},
			other: &ForeignKey{
				Name:          "",
				ChildColumns:  []string{"employee_id"},
				ParentTable:   "employee",
				ParentColumns: []string{"id"},
				OnDelete:      "",
			},
			expected: true,
		},
		{
			name: "db has name, spec has matching auto-generated name pattern",
			fk: &ForeignKey{
				Name:          "assignment_department_id_fkey",
				ChildColumns:  []string{"department_id"},
				ParentTable:   "department",
				ParentColumns: []string{"id"},
				OnDelete:      "CASCADE",
			},
			other: &ForeignKey{
				Name:          "",
				ChildColumns:  []string{"department_id"},
				ParentTable:   "department",
				ParentColumns: []string{"id"},
				OnDelete:      "cascade",
			},
			expected: true,
		},
		{
			name: "different parent table",
			fk: &ForeignKey{
				Name:          "fk1",
				ChildColumns:  []string{"col1"},
				ParentTable:   "table_a",
				ParentColumns: []string{"id"},
				OnDelete:      "CASCADE",
			},
			other: &ForeignKey{
				Name:          "fk1",
				ChildColumns:  []string{"col1"},
				ParentTable:   "table_b",
				ParentColumns: []string{"id"},
				OnDelete:      "CASCADE",
			},
			expected: false,
		},
		{
			name: "different child columns",
			fk: &ForeignKey{
				Name:          "fk1",
				ChildColumns:  []string{"col_a"},
				ParentTable:   "parent",
				ParentColumns: []string{"id"},
			},
			other: &ForeignKey{
				Name:          "fk1",
				ChildColumns:  []string{"col_b"},
				ParentTable:   "parent",
				ParentColumns: []string{"id"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fk.Equals(tt.other)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test_QualifyParentTableToSchema verifies that QualifyParentTableToSchema
// qualifies a bare reference with the FK table's schema, leaves an already-
// qualified reference unchanged, and is a no-op when schema is empty (public /
// default schema). This is the helper used in BuildForeignKeyStatements to make
// a bare declared "cells" compare equal to the live "global.cells" so the
// planner emits zero DROP+ADD for a satisfied FK on a global-schema table.
func Test_QualifyParentTableToSchema(t *testing.T) {
	tests := []struct {
		name        string
		parentTable string
		schema      string
		want        string
	}{
		{
			name:        "bare table with non-public schema is qualified",
			parentTable: "cells",
			schema:      "global",
			want:        "global.cells",
		},
		{
			name:        "already-qualified reference is left unchanged",
			parentTable: "global.cells",
			schema:      "global",
			want:        "global.cells",
		},
		{
			name:        "empty schema (public default) is a no-op",
			parentTable: "cells",
			schema:      "",
			want:        "cells",
		},
		{
			name:        "explicitly qualified to a different schema is preserved",
			parentTable: "other.cells",
			schema:      "global",
			want:        "other.cells",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, QualifyParentTableToSchema(tt.parentTable, tt.schema))
		})
	}
}

// Test_ForeignKeyEquals_schemaQualifiedReference verifies the end-to-end FK
// comparison scenario for global-schema tables: the live FK from
// ListTableForeignKeys has ParentTable="global.cells" (schema-qualified), while
// the desired FK from the CRD spec has ParentTable="cells" (bare). After
// qualifying the desired FK with QualifyParentTableToSchema, Equals returns
// true and the planner emits no DROP+ADD for tenant_assignments_cell_id_fkey.
//
// This is the FK analogue of the schema-qualified enum cast canonicalization
// that FIX A addresses for CHECK constraints and partial indexes.
func Test_ForeignKeyEquals_schemaQualifiedReference(t *testing.T) {
	tests := []struct {
		name    string
		live    *ForeignKey // from ListTableForeignKeys (ParentTable schema-qualified)
		desired *ForeignKey // from spec (ParentTable bare), qualified via helper
		schema  string      // FK table's schema
		want    bool
	}{
		{
			// tenant_assignments_cell_id_fkey: declared "cells", live "global.cells".
			// After qualifying the declared reference with the FK table's schema
			// ("global"), both sides have "global.cells" and Equals returns true.
			name: "bare declared reference matches schema-qualified live reference after qualification",
			live: &ForeignKey{
				Name:          "tenant_assignments_cell_id_fkey",
				ChildColumns:  []string{"cell_id"},
				ParentTable:   "global.cells",
				ParentColumns: []string{"id"},
			},
			desired: &ForeignKey{
				Name:          "tenant_assignments_cell_id_fkey",
				ChildColumns:  []string{"cell_id"},
				ParentTable:   "cells", // bare as declared in CRD
				ParentColumns: []string{"id"},
			},
			schema: "global",
			want:   true,
		},
		{
			// public-schema FK: schema is empty, so QualifyParentTableToSchema is
			// a no-op and existing public-schema behaviour is preserved.
			name: "public-schema FK: qualification is a no-op, behavior unchanged",
			live: &ForeignKey{
				Name:          "orders_customer_id_fkey",
				ChildColumns:  []string{"customer_id"},
				ParentTable:   "customers",
				ParentColumns: []string{"id"},
			},
			desired: &ForeignKey{
				Name:          "orders_customer_id_fkey",
				ChildColumns:  []string{"customer_id"},
				ParentTable:   "customers",
				ParentColumns: []string{"id"},
			},
			schema: "",
			want:   true,
		},
		{
			// A genuinely different referenced table must not compare equal even after
			// qualification — the planner must still emit a DROP+ADD.
			name: "different referenced table is not equal after qualification",
			live: &ForeignKey{
				Name:          "tenant_assignments_cell_id_fkey",
				ChildColumns:  []string{"cell_id"},
				ParentTable:   "global.cells",
				ParentColumns: []string{"id"},
			},
			desired: &ForeignKey{
				Name:          "tenant_assignments_cell_id_fkey",
				ChildColumns:  []string{"cell_id"},
				ParentTable:   "other_table",
				ParentColumns: []string{"id"},
			},
			schema: "global",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate what BuildForeignKeyStatements does: qualify the desired
			// FK's ParentTable before calling Equals.
			qualified := *tt.desired
			qualified.ParentTable = QualifyParentTableToSchema(tt.desired.ParentTable, tt.schema)
			assert.Equal(t, tt.want, tt.live.Equals(&qualified))
		})
	}
}
