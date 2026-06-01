/*
Copyright 2019 The SchemaHero Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha4

// +kubebuilder:validation:ExactlyOneOf=execute;executeProcedure
type PostgresqlTableTrigger struct {
	Name              string                         `json:"name,omitempty" yaml:"name,omitempty"`
	ConstraintTrigger *bool                          `json:"constraintTrigger,omitempty" yaml:"constraintTrigger,omitempty"`
	Events            []string                       `json:"events" yaml:"events"`
	ForEachStatement  *bool                          `json:"forEachStatement,omitempty" yaml:"forEachStatement,omitempty"`
	ForEachRow        *bool                          `json:"forEachRun,omitempty" yaml:"forEachRow,omitempty"`
	Condition         *string                        `json:"condition,omitempty" yaml:"condition,omitempty"`
	Execute           *PostgresqlTableTriggerExecute `json:"execute,omitempty" yaml:"execute,omitempty"`
	// Deprecated: we support multiple execute types from now on.
	// You are encouraged to use Execute instead.
	ExecuteProcedure string `json:"executeProcedure,omitempty" yaml:"executeProcedure,omitempty"`
}

type PostgresqlTableTriggerExecute struct {
	//+kubebuilder:validation:Enum=Procedure;Function
	//+kubebuilder:default:=Procedure
	Type   string `json:"type" yaml:"type"`
	Schema string `json:"schema,omitempty" yaml:"schema,omitempty"`
	Name   string `json:"name" yaml:"name"`
	// +kubebuilder:validation:MaxItems=100
	Params []*PostgresqlExecuteParameter `json:"params,omitempty" yaml:"params,omitempty"`
}

type PostgresqlTableForeignKeyReferences struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
}

type PostgresqlTableForeignKey struct {
	Columns    []string                            `json:"columns" yaml:"columns"`
	References PostgresqlTableForeignKeyReferences `json:"references" yaml:"references"`
	OnDelete   string                              `json:"onDelete,omitempty" yaml:"onDelete,omitempty"`
	Name       string                              `json:"name,omitempty" yaml:"name,omitempty"`
}

// PostgresqlTableCheckConstraint is a table-level CHECK constraint.
type PostgresqlTableCheckConstraint struct {
	// Name is the constraint name. Optional; if empty a deterministic name
	// "<bareTable>_<sanitized-expr>_check" is generated. Setting Name explicitly
	// is strongly recommended: expression-derived names are fragile (near-identical
	// expressions can collide, and long expressions exceed PostgreSQL's 63-char
	// identifier limit, silently truncating and breaking the diff).
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// Expression is the raw boolean SQL placed inside CHECK ( ... ), e.g.
	// "age >= 0" or "status in ('a','b')". It is emitted verbatim (it is already
	// SQL, like an FK references clause) and is NOT quoted or escaped.
	Expression string `json:"expression" yaml:"expression"`
}

// PostgresqlTableExclusionConstraintItem is one "element WITH operator" pair of an
// EXCLUDE constraint, e.g. {Column: "room_id", Operator: "="} or
// {Expression: "tstzrange(starts_at, ends_at)", Operator: "&&"}. Exactly one of
// Column or Expression must be set.
// +kubebuilder:validation:ExactlyOneOf=column;expression
type PostgresqlTableExclusionConstraintItem struct {
	// Column is a plain column reference. It is identifier-quoted on emit.
	Column string `json:"column,omitempty" yaml:"column,omitempty"`
	// Expression is a raw SQL element expression, e.g. "tstzrange(starts_at, ends_at)".
	// It is emitted verbatim inside parentheses (never identifier-quoted).
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty"`
	// Operator is the PostgreSQL operator token used to compare this element across
	// rows, e.g. "=", "&&", "<@". It is emitted raw (it is an operator, not an identifier).
	Operator string `json:"operator" yaml:"operator"`
}

// PostgresqlTableExclusionConstraint is a table-level EXCLUDE constraint, e.g.
// EXCLUDE USING gist (room_id WITH =, during WITH &&) WHERE (room_id IS NOT NULL).
//
// An EXCLUDE present on the table but absent from PostgresqlTableSchema.ExclusionConstraints
// IS DROPPED on apply (the same authoritative behavior as foreignKeys, checks, and
// indexes). EXCLUDE constraints with scalar equality operators under the default gist
// access method require the btree_gist extension; SchemaHero does NOT install it.
type PostgresqlTableExclusionConstraint struct {
	// Name is the constraint name. Optional; if empty a deterministic name
	// "<bareTable>_<element>_excl" is generated. Setting Name explicitly is
	// strongly recommended (generated names can collide or exceed the 63-char limit).
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// Using is the index access method backing the constraint; defaults to gist.
	Using string `json:"using,omitempty" yaml:"using,omitempty"`
	// Items is the ordered list of "element WITH operator" pairs. Order is
	// significant and is compared order-sensitively by the diff.
	Items []*PostgresqlTableExclusionConstraintItem `json:"items" yaml:"items"`
	// Where is a partial-constraint predicate (raw SQL, without the WHERE keyword),
	// e.g. "room_id IS NOT NULL". Emitted verbatim.
	Where string `json:"where,omitempty" yaml:"where,omitempty"`
	// With holds index storage parameters, e.g. {fillfactor: "70"}. These are
	// emitted on CREATE but NOT compared by the diff (introspection cannot cheaply
	// recover them), matching how the FK/index diffs compare structurally.
	With map[string]string `json:"with,omitempty" yaml:"with,omitempty"`
}

type PostgresqlTableIndex struct {
	Columns  []string `json:"columns,omitempty" yaml:"columns,omitempty"`
	Name     string   `json:"name,omitempty" yaml:"name,omitempty"`
	IsUnique bool     `json:"isUnique,omitempty" yaml:"isUnique,omitempty"`
	// Type is the index method (access method): btree (default) | hash | gin | gist | brin | spgist.
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// With holds index storage parameters, e.g. {fillfactor: "70"}.
	With map[string]string `json:"with,omitempty" yaml:"with,omitempty"`
	// Where is a partial-index predicate (raw SQL, without the WHERE keyword), e.g. "phone <> ''".
	Where string `json:"where,omitempty" yaml:"where,omitempty"`
	// Expressions are functional-index column expressions (raw SQL), e.g. ["lower(email)"].
	Expressions []string `json:"expressions,omitempty" yaml:"expressions,omitempty"`
	// SortedColumns are index columns with explicit ordering (ASC/DESC, NULLS FIRST/LAST).
	SortedColumns []*PostgresqlTableIndexColumn `json:"sortedColumns,omitempty" yaml:"sortedColumns,omitempty"`
}

type PostgresqlTableIndexColumn struct {
	Column string `json:"column" yaml:"column"`
	//+kubebuilder:validation:Enum=ASC;DESC
	Sort string `json:"sort,omitempty" yaml:"sort,omitempty"`
	//+kubebuilder:validation:Enum=FIRST;LAST
	Nulls string `json:"nulls,omitempty" yaml:"nulls,omitempty"`
}

type PostgresqlTableColumnConstraints struct {
	NotNull *bool `json:"notNull,omitempty" yaml:"notNull,omitempty"`
}

type PostgresqlTableColumnAttributes struct {
	AutoIncrement *bool `json:"autoIncrement,omitempty" yaml:"autoIncrement,omitempty"`
}

type PostgresqlTableColumn struct {
	Name        string                            `json:"name" yaml:"name"`
	Type        string                            `json:"type" yaml:"type"`
	Constraints *PostgresqlTableColumnConstraints `json:"constraints,omitempty" yaml:"constraints,omitempty"`
	Attributes  *PostgresqlTableColumnAttributes  `json:"attributes,omitempty" yaml:"attributes,omitempty"`
	Default     *string                           `json:"default,omitempty" yaml:"default,omitempty"`
}

type PostgresqlTableSchema struct {
	Schema      string                       `json:"schema,omitempty" yaml:"schema,omitempty"`
	PrimaryKey  []string                     `json:"primaryKey,omitempty" yaml:"primaryKey,omitempty"`
	ForeignKeys []*PostgresqlTableForeignKey `json:"foreignKeys,omitempty" yaml:"foreignKeys,omitempty"`
	// Checks is the authoritative list of table-level CHECK constraints. A CHECK
	// present on the table but absent from this list IS DROPPED on apply (the same
	// authoritative behavior as foreignKeys and indexes).
	// +kubebuilder:validation:MaxItems=100
	Checks  []*PostgresqlTableCheckConstraint `json:"checks,omitempty" yaml:"checks,omitempty"`
	Indexes []*PostgresqlTableIndex           `json:"indexes,omitempty" yaml:"indexes,omitempty"`
	// ExclusionConstraints is the authoritative list of table-level EXCLUDE
	// constraints. An EXCLUDE present on the table but absent from this list IS
	// DROPPED on apply (the same authoritative behavior as foreignKeys, checks,
	// and indexes).
	// +kubebuilder:validation:MaxItems=100
	ExclusionConstraints []*PostgresqlTableExclusionConstraint `json:"exclusionConstraints,omitempty" yaml:"exclusionConstraints,omitempty"`
	Columns              []*PostgresqlTableColumn              `json:"columns,omitempty" yaml:"columns,omitempty"`
	IsDeleted            bool                                  `json:"isDeleted,omitempty" yaml:"isDeleted,omitempty"`
	// Deprecated: this field should be avoided and one should use Triggers without json prefix instead
	// +kubebuilder:validation:MaxItems=100
	JSONTriggers []*PostgresqlTableTrigger `json:"json:triggers,omitempty" yaml:"json:triggers,omitempty"`
	// +kubebuilder:validation:MaxItems=100
	Triggers []*PostgresqlTableTrigger `json:"triggers,omitempty" yaml:"triggers,omitempty"`
}

type PostgresqlFunctionSchema struct {
	// Schema is the schema the function should be saved in
	Schema string `json:"schema,omitempty" yaml:"schema,omitempty"`
	//+kubebuilder:validation:Enum=PLpgSQL;SQL
	//+kubebuilder:default:=PLpgSQL
	Lang string `json:"lang" yaml:"lang"`
	// Params is a mapping between function parameter name and its respective type
	Params []*PostgresqlExecuteParameter `json:"params,omitempty" yaml:"params,omitempty"`
	// ReturnSet tells if the returned value is a set or not
	ReturnSet bool `json:"returnSet,omitempty" yaml:"returnSet,omitempty"`
	// Return, if defined, tells what type to return
	Return string `json:"return,omitempty" yaml:"return,omitempty"`
	// As represents the function logic. An example looks as follows:
	// ```
	// DECLARE
	//     user_count bigint;
	// BEGIN
	//     SELECT COUNT(*) INTO user_count FROM users;
	//     RETURN user_count;
	// END;
	// ```
	As string `json:"as" yaml:"as"`
	// SecurityDefiner runs the function with the privileges of its owner
	// (SECURITY DEFINER) rather than the caller (the default, SECURITY INVOKER).
	SecurityDefiner bool `json:"securityDefiner,omitempty" yaml:"securityDefiner,omitempty"`
	// Aliases for compatibility
	Body     string `json:"-" yaml:"-"`
	Returns  string `json:"-" yaml:"-"`
	Language string `json:"-" yaml:"-"`
	// IsDeleted is used internally to mark function for deletion during planning
	IsDeleted bool `json:"-" yaml:"-"`
}

type PostgresqlExecuteParameter struct {
	//+kubebuilder:validation:Enum=IN;OUT;INOUT;VARIADIC
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	Type string `json:"type" yaml:"type"`
}
