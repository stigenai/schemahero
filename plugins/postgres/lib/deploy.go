package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"
)

func PlanPostgresView(uri string, viewName string, postgresViewSchema *schemasv1alpha4.NotImplementedViewSchema) ([]string, error) {
	return nil, errors.New("not implemented")
}

func PlanCockroachDBTable(uri string, tableName string, cockroachTableSchema *schemasv1alpha4.PostgresqlTableSchema, seedData *schemasv1alpha4.SeedData) ([]string, error) {
	// CockroachDB uses PostgreSQL-compatible SQL
	// For now, return an error until properly implemented
	// TODO: Implement CockroachDB-specific features
	return nil, errors.New("CockroachDB planning not yet implemented")
}

func PlanPostgresFunction(uri string, functionName string, postgresFunctionSchema *schemasv1alpha4.PostgresqlFunctionSchema) ([]string, error) {
	p, err := Connect(uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to postgres")
	}
	defer p.Close()

	// introspect the current definition (body-aware so we can diff)
	existingDef, functionExists, err := GetFunctionDefinition(p, postgresFunctionSchema.Schema, functionName, postgresFunctionSchema.Params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get function definition")
	}

	// DROP path: the controller finalizer sets IsDeleted. Only emit a destructive
	// drop when introspection confirms the function exists; the statement itself
	// also carries "if exists" as belt-and-suspenders.
	if postgresFunctionSchema.IsDeleted {
		if !functionExists {
			return []string{}, nil
		}
		return DropFunctionStatements(functionName, postgresFunctionSchema), nil
	}

	desired := CreateFunctionStatements(functionName, postgresFunctionSchema)

	if !functionExists {
		return desired, nil
	}

	// The function exists. "create or replace" is itself idempotent at the DB
	// layer, so this body-diff is purely a plan-noise optimization: when the
	// existing definition already matches what we would emit, produce nothing so
	// a re-plan is empty. A false negative just re-runs a harmless no-op REPLACE.
	if functionDefinitionsEqual(existingDef, postgresFunctionSchema) {
		return []string{}, nil
	}

	// Changed body/attributes: CREATE OR REPLACE re-applies in place (no drop),
	// preserving grants/dependencies. (REPLACE cannot change the return type or
	// drop OUT params; if those changed Postgres errors 42P13 loudly — we do NOT
	// auto-drop to work around it.)
	return desired, nil
}

func PlanPostgresTable(uri string, tableName string, postgresTableSchema *schemasv1alpha4.PostgresqlTableSchema, seedData *schemasv1alpha4.SeedData) ([]string, error) {
	p, err := Connect(uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to postgres")
	}
	defer p.Close()

	// determine if the table exists
	tableExists, err := CheckIfTableExists(p, tableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check if table exists")
	}

	if !tableExists && postgresTableSchema.IsDeleted {
		return []string{}, nil
	} else if tableExists && postgresTableSchema.IsDeleted {
		return []string{
			fmt.Sprintf(`drop table %s`, sanitizeTableName(tableName)),
		}, nil
	}

	seedDataStatements := []string{}
	if seedData != nil {
		seedDataStatements, err = SeedDataStatements(tableName, postgresTableSchema, seedData)
		if err != nil {
			return nil, errors.Wrap(err, "create seed data statements")
		}
	}

	if !tableExists {
		// shortcut to just create it
		queries, err := CreateTableStatements(tableName, postgresTableSchema)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create table statement")
		}
		queries = append(queries, seedDataStatements...)

		indexStatements, err := BuildIndexStatements(p, tableName, postgresTableSchema)
		if err != nil {
			return nil, errors.Wrap(err, "failed to build index statements")
		}
		queries = append(queries, indexStatements...)

		return queries, nil
	}

	statements := []string{}

	// table needs to be altered?
	columnStatements, err := BuildColumnStatements(p, tableName, postgresTableSchema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build column statement")
	}
	statements = append(statements, columnStatements...)

	// primary key changes
	primaryKeyStatements, err := BuildPrimaryKeyStatements(p, tableName, postgresTableSchema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build primary key statements")
	}
	statements = append(statements, primaryKeyStatements...)

	// foreign key changes
	foreignKeyStatements, err := BuildForeignKeyStatements(p, tableName, postgresTableSchema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build foreign key statements")
	}
	statements = append(statements, foreignKeyStatements...)

	// check constraint changes
	checkConstraintStatements, err := BuildCheckConstraintStatements(p, tableName, postgresTableSchema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build check constraint statements")
	}
	statements = append(statements, checkConstraintStatements...)

	// index changes
	indexStatements, err := BuildIndexStatements(p, tableName, postgresTableSchema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build index statements")
	}
	statements = append(statements, indexStatements...)

	// exclusion constraint changes (emitted after indexes: an EXCLUDE depends on
	// the table's columns existing and behaves like the other index-backed objects)
	exclusionConstraintStatements, err := BuildExclusionConstraintStatements(p, tableName, postgresTableSchema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build exclusion constraint statements")
	}
	statements = append(statements, exclusionConstraintStatements...)

	statements = append(statements, seedDataStatements...)

	return statements, nil
}

// PlanPostgresTableSeedDataOnly generates SQL statements for seed data without a schema definition.
// This function connects to the database to retrieve the existing table schema,
// then generates seed data statements based on that schema.
func PlanPostgresTableSeedDataOnly(uri string, tableName string, seedData *schemasv1alpha4.SeedData) ([]string, error) {
	if seedData == nil {
		return []string{}, nil
	}

	p, err := Connect(uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to postgres")
	}
	defer p.Close()

	// Check if the table exists
	tableExists, err := CheckIfTableExists(p, tableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check if table exists")
	}

	if !tableExists {
		return nil, errors.Errorf("table %s does not exist, cannot apply seed data without schema", tableName)
	}

	// Get the existing table schema from the database
	existingColumns, err := p.GetTableSchema(tableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get existing table schema")
	}

	// Get the primary key for conflict resolution
	primaryKey, err := p.GetTablePrimaryKey(tableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get table primary key")
	}

	// Convert the existing columns to a PostgresqlTableSchema
	postgresSchema := &schemasv1alpha4.PostgresqlTableSchema{
		Columns: []*schemasv1alpha4.PostgresqlTableColumn{},
	}

	// Add primary key if it exists
	if primaryKey != nil {
		postgresSchema.PrimaryKey = primaryKey.Columns
	}

	for _, col := range existingColumns {
		postgresCol := &schemasv1alpha4.PostgresqlTableColumn{
			Name: col.Name,
			Type: col.DataType,
		}

		if col.Constraints != nil && col.Constraints.NotNull != nil {
			postgresCol.Constraints = &schemasv1alpha4.PostgresqlTableColumnConstraints{
				NotNull: col.Constraints.NotNull,
			}
		}

		if col.ColumnDefault != nil {
			postgresCol.Default = col.ColumnDefault
		}

		postgresSchema.Columns = append(postgresSchema.Columns, postgresCol)
	}

	// Generate seed data statements
	seedDataStatements, err := SeedDataStatements(tableName, postgresSchema, seedData)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create seed data statements")
	}

	return seedDataStatements, nil
}

func DeployPostgresStatements(uri string, statements []string) error {
	p, err := Connect(uri)
	if err != nil {
		return err
	}
	defer p.Close()

	// execute
	if err := executeStatements(p, statements); err != nil {
		return err
	}

	return nil
}

func executeStatements(p *PostgresConnection, statements []string) error {
	for _, statement := range statements {
		if statement == "" {
			continue
		}
		// Statement is already printed by the main process
		if _, err := p.conn.Exec(context.Background(), statement); err != nil {
			return err
		}
	}

	return nil
}

func BuildColumnStatements(p *PostgresConnection, tableName string, postgresTableSchema *schemasv1alpha4.PostgresqlTableSchema) ([]string, error) {
	schema, table := splitQualifiedTableName(tableName)
	query := `select
column_name, column_default, is_nullable, data_type, udt_name, character_maximum_length
from information_schema.columns
where table_name = $1`
	args := []interface{}{table}
	if schema != "" {
		query += ` and table_schema = $2`
		args = append(args, schema)
	}
	rows, err := p.conn.Query(context.Background(), query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to select from information_schema")
	}
	defer rows.Close()

	alterAndDropStatements := []string{}
	foundColumnNames := []string{}
	for rows.Next() {
		var columnName, dataType, udtName, isNullable string
		var columnDefault sql.NullString
		var charMaxLength sql.NullInt64

		if err := rows.Scan(&columnName, &columnDefault, &isNullable, &dataType, &udtName, &charMaxLength); err != nil {
			return nil, errors.Wrap(err, "failed to scan")
		}

		foundColumnNames = append(foundColumnNames, columnName)

		existingColumn := types.Column{
			Name:        columnName,
			DataType:    dataType,
			Constraints: &types.ColumnConstraints{},
		}

		switch dataType {
		case "ARRAY":
			existingColumn.IsArray = true
			existingColumn.DataType = UDTNameToDataType(udtName)
		case "USER-DEFINED":
			existingColumn.DataType = UDTNameToDataType(udtName)
		}

		if isNullable == "NO" {
			existingColumn.Constraints.NotNull = &trueValue
		} else {
			existingColumn.Constraints.NotNull = &falseValue
		}

		if columnDefault.Valid {
			value := stripOIDClass(columnDefault.String)
			existingColumn.ColumnDefault = &value
		}
		if charMaxLength.Valid {
			existingColumn.DataType = fmt.Sprintf("%s (%d)", existingColumn.DataType, charMaxLength.Int64)
		}

		columnStatement, err := AlterColumnStatements(tableName, postgresTableSchema.PrimaryKey, postgresTableSchema.Columns, &existingColumn)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create alter column statement")
		}

		alterAndDropStatements = append(alterAndDropStatements, columnStatement...)
	}

	for _, desiredColumn := range postgresTableSchema.Columns {
		isColumnPresent := false
		for _, foundColumn := range foundColumnNames {
			if foundColumn == desiredColumn.Name {
				isColumnPresent = true
			}
		}

		if !isColumnPresent {
			statement, err := InsertColumnStatement(tableName, desiredColumn)
			if err != nil {
				return nil, errors.Wrap(err, "failed to create insert column statement")
			}

			alterAndDropStatements = append(alterAndDropStatements, statement)
		}
	}

	return alterAndDropStatements, nil
}

func BuildPrimaryKeyStatements(p *PostgresConnection, tableName string, postgresTableSchema *schemasv1alpha4.PostgresqlTableSchema) ([]string, error) {
	currentPrimaryKey, err := p.GetTablePrimaryKey(tableName)
	if err != nil {
		return nil, err
	}
	var postgresTableSchemaPrimaryKey *types.KeyConstraint
	if len(postgresTableSchema.PrimaryKey) > 0 {
		postgresTableSchemaPrimaryKey = &types.KeyConstraint{
			IsPrimary: true,
			Columns:   postgresTableSchema.PrimaryKey,
		}
	}

	if postgresTableSchemaPrimaryKey.Equals(currentPrimaryKey) {
		return nil, nil
	}

	var statements []string
	if currentPrimaryKey != nil {
		statements = append(statements, RemoveConstrantStatement(tableName, currentPrimaryKey))
	}

	if postgresTableSchemaPrimaryKey != nil {
		statements = append(statements, AddConstrantStatement(tableName, postgresTableSchemaPrimaryKey))
	}

	return statements, nil
}

func BuildForeignKeyStatements(p *PostgresConnection, tableName string, postgresTableSchema *schemasv1alpha4.PostgresqlTableSchema) ([]string, error) {
	foreignKeyStatements := []string{}
	droppedKeys := []string{}
	currentForeignKeys, err := p.ListTableForeignKeys(p.databaseName, tableName)
	if err != nil {
		return nil, err
	}

	for _, foreignKey := range postgresTableSchema.ForeignKeys {
		var statement string
		var matchedForeignKey *types.ForeignKey
		for _, currentForeignKey := range currentForeignKeys {
			if currentForeignKey.Equals(types.PostgresqlSchemaForeignKeyToForeignKey(foreignKey)) {
				goto Next
			}

			matchedForeignKey = currentForeignKey
		}

		// drop and readd?  is this always ok
		// TODO can we alter
		if matchedForeignKey != nil {
			statement = RemoveForeignKeyStatement(tableName, matchedForeignKey)
			droppedKeys = append(droppedKeys, matchedForeignKey.Name)
			foreignKeyStatements = append(foreignKeyStatements, statement)
		}

		statement = AddForeignKeyStatement(tableName, foreignKey)
		foreignKeyStatements = append(foreignKeyStatements, statement)

	Next:
	}

	for _, currentForeignKey := range currentForeignKeys {
		var statement string
		for _, foreignKey := range postgresTableSchema.ForeignKeys {
			if currentForeignKey.Equals(types.PostgresqlSchemaForeignKeyToForeignKey(foreignKey)) {
				goto NextCurrentFK
			}
		}

		for _, droppedKey := range droppedKeys {
			if droppedKey == currentForeignKey.Name {
				goto NextCurrentFK
			}
		}

		statement = RemoveForeignKeyStatement(tableName, currentForeignKey)
		foreignKeyStatements = append(foreignKeyStatements, statement)

	NextCurrentFK:
	}

	return foreignKeyStatements, nil
}

func BuildIndexStatements(p *PostgresConnection, tableName string, postgresTableSchema *schemasv1alpha4.PostgresqlTableSchema) ([]string, error) {
	indexStatements := []string{}
	droppedIndexes := []string{}
	currentIndexes, err := p.ListTableIndexes(p.databaseName, tableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list table indexes")
	}
	currentConstraints, err := p.ListTableConstraints(p.databaseName, tableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list table constraints")
	}

	tableExists, err := CheckIfTableExists(p, tableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check if table exists")
	}

DesiredIndexLoop:
	for _, index := range postgresTableSchema.Indexes {
		// Skip unique indexes for new tables as they're already added as inline
		// UNIQUE constraints in CREATE TABLE. Extended unique indexes (method,
		// partial, expression, or ordered) cannot be inline constraints and so are
		// NOT skipped here; they are emitted as standalone CREATE UNIQUE INDEX.
		if !tableExists && isInlineFoldableUniqueIndex(index) {
			continue
		}

		if index.Name == "" {
			index.Name = types.GeneratePostgresqlIndexName(tableName, index)
		}

		var statement string
		var matchedIndex *types.Index
		for _, currentIndex := range currentIndexes {
			if currentIndex.Equals(types.PostgresqlSchemaIndexToIndex(index)) {
				continue DesiredIndexLoop
			}

			if currentIndex.Name == index.Name {
				matchedIndex = currentIndex
			}
		}

		// drop and readd? pg supports a little bit of alter index we should support (rename)
		if matchedIndex != nil {
			isConstraint := false
			for _, currentConstraint := range currentConstraints {
				if matchedIndex.Name == currentConstraint {
					isConstraint = true
				}
			}

			if isConstraint {
				statement = RemoveConstraintStatement(tableName, matchedIndex)
			} else {
				statement = RemoveIndexStatement(tableName, matchedIndex)
			}
			droppedIndexes = append(droppedIndexes, matchedIndex.Name)
			indexStatements = append(indexStatements, statement)
		}

		statement = AddIndexStatement(tableName, index)
		indexStatements = append(indexStatements, statement)
	}

ExistingIndexLoop:
	for _, currentIndex := range currentIndexes {
		var statement string
		isConstraint := false

		for _, index := range postgresTableSchema.Indexes {
			if currentIndex.Equals(types.PostgresqlSchemaIndexToIndex(index)) {
				continue ExistingIndexLoop
			}
		}

		for _, droppedIndex := range droppedIndexes {
			if droppedIndex == currentIndex.Name {
				continue ExistingIndexLoop
			}
		}

		for _, currentConstraint := range currentConstraints {
			if currentIndex.Name == currentConstraint {
				isConstraint = true
			}
		}

		if isConstraint {
			statement = RemoveConstraintStatement(tableName, currentIndex)
		} else {
			statement = RemoveIndexStatement(tableName, currentIndex)
		}

		indexStatements = append(indexStatements, statement)
	}

	return indexStatements, nil
}

// CheckIfTableExists returns whether the specified table exists in the database
func CheckIfTableExists(p *PostgresConnection, tableName string) (bool, error) {
	schema, table := splitQualifiedTableName(tableName)
	query := `select count(1) from information_schema.tables where table_name = $1`
	args := []interface{}{table}
	if schema != "" {
		query += ` and table_schema = $2`
		args = append(args, schema)
	}
	row := p.conn.QueryRow(context.Background(), query, args...)
	tableExists := 0
	if err := row.Scan(&tableExists); err != nil {
		return false, errors.Wrap(err, "failed to scan")
	}

	return tableExists > 0, nil
}

// GetFunctionDefinition returns (definition, exists, error). definition is the
// canonical "CREATE OR REPLACE FUNCTION ..." text from pg_get_functiondef, or
// "" when the function is absent.
//
// The function is located by its identity signature (schema, name, input-arg
// types) via to_regprocedure. to_regprocedure is used in preference to a
// regprocedure cast because the cast THROWS 42883 on a missing function,
// whereas to_regprocedure returns NULL, so an absent function cleanly yields
// exists=false (and an unqualified name is resolved search_path-aware, fixing
// the previous empty-routine_schema default-schema bug).
func GetFunctionDefinition(p *PostgresConnection, functionSchema string, functionName string, params []*schemasv1alpha4.PostgresqlExecuteParameter) (string, bool, error) {
	signature := getFunctionSignature(functionName, functionSchema, params)

	query := `select pg_get_functiondef(p.oid)
from pg_proc p
where p.oid = to_regprocedure($1)`
	row := p.conn.QueryRow(context.Background(), query, signature)

	var def sql.NullString
	if err := row.Scan(&def); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, errors.Wrap(err, "failed to scan function definition")
	}
	if !def.Valid {
		return "", false, nil
	}

	return def.String, true, nil
}

// CheckIfFunctionExists returns whether the specified function exists in the
// database. It is a thin wrapper over GetFunctionDefinition so existence and the
// body diff derive from the same overload-correct lookup.
func CheckIfFunctionExists(p *PostgresConnection, functionSchema string, functionName string, params []*schemasv1alpha4.PostgresqlExecuteParameter) (bool, error) {
	_, exists, err := GetFunctionDefinition(p, functionSchema, functionName, params)
	return exists, err
}

// functionDefinitionsEqual reports whether the canonical pg_get_functiondef text
// (existingDef) already represents the desired schema, so a no-op REPLACE can be
// suppressed from the plan.
//
// pg_get_functiondef normalizes its output (its own dollar-tag, whitespace,
// lowercased language, AS placement), so an exact string compare against our
// generated DDL never matches. Instead we compare structurally: the body between
// the existing dollar-quote delimiters vs s.As (both trimmed), the language
// (case-insensitive), and the presence of SECURITY DEFINER.
//
// It is intentionally conservative: when the existing body cannot be extracted
// it returns false, re-emitting the (DB-level idempotent) CREATE OR REPLACE.
// Correctness does NOT depend on this function — only plan cleanliness does.
func functionDefinitionsEqual(existingDef string, s *schemasv1alpha4.PostgresqlFunctionSchema) bool {
	existingBody, ok := extractDollarQuotedBody(existingDef)
	if !ok {
		return false
	}
	if strings.TrimSpace(existingBody) != strings.TrimSpace(s.As) {
		return false
	}

	// Language: pg_get_functiondef emits e.g. "LANGUAGE plpgsql".
	if !strings.Contains(strings.ToLower(existingDef), "language "+strings.ToLower(s.Lang)) {
		return false
	}

	// SECURITY DEFINER presence must match. pg_get_functiondef only emits
	// "SECURITY DEFINER" when set (the default INVOKER is omitted).
	existingHasDefiner := strings.Contains(strings.ToUpper(existingDef), "SECURITY DEFINER")
	if existingHasDefiner != s.SecurityDefiner {
		return false
	}

	return true
}

// extractDollarQuotedBody returns the text between the first pair of matching
// dollar-quote delimiters (e.g. $function$ ... $function$) in a function
// definition, and whether such a pair was found. pg_get_functiondef always
// dollar-quotes the body, so this recovers it independent of the tag chosen.
func extractDollarQuotedBody(def string) (string, bool) {
	open := strings.Index(def, "$")
	if open < 0 {
		return "", false
	}
	// The delimiter runs from the first '$' to the next '$' inclusive, e.g.
	// "$function$" or "$$".
	tagEnd := strings.Index(def[open+1:], "$")
	if tagEnd < 0 {
		return "", false
	}
	delimiter := def[open : open+1+tagEnd+1]

	bodyStart := open + len(delimiter)
	end := strings.Index(def[bodyStart:], delimiter)
	if end < 0 {
		return "", false
	}
	return def[bodyStart : bodyStart+end], true
}

// CheckIfExtensionExists returns whether the specified extension exists in the database
func CheckIfExtensionExists(p *PostgresConnection, extensionName string) (bool, error) {
	query := `select count(1) from pg_extension where extname = $1`
	row := p.conn.QueryRow(context.Background(), query, extensionName)
	extensionExists := 0
	if err := row.Scan(&extensionExists); err != nil {
		return false, errors.Wrap(err, "failed to scan")
	}

	return extensionExists > 0, nil
}

func PlanPostgresExtension(uri string, extensionName string, postgresExtensionSchema *schemasv1alpha4.PostgresDatabaseExtension) ([]string, error) {
	p, err := Connect(uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to postgres")
	}
	defer p.Close()

	extensionExists, err := CheckIfExtensionExists(p, extensionName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check if extension exists")
	}

	queries := []string{}

	if !extensionExists && postgresExtensionSchema.IsDeleted {
		return []string{}, nil
	} else if extensionExists && postgresExtensionSchema.IsDeleted {
		dropStatements, err := DropExtensionStatements([]*schemasv1alpha4.PostgresDatabaseExtension{postgresExtensionSchema})
		if err != nil {
			return nil, errors.Wrap(err, "failed to create drop extension statements")
		}
		return dropStatements, nil
	}

	if !extensionExists {
		createStatements, err := CreateExtensionStatements([]*schemasv1alpha4.PostgresDatabaseExtension{postgresExtensionSchema})
		if err != nil {
			return nil, errors.Wrap(err, "failed to create extension statements")
		}
		queries = append(queries, createStatements...)
	}

	return queries, nil
}
