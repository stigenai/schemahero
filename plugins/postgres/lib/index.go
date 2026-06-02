package postgres

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v4"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"
)

func RemoveConstraintStatement(tableName string, index *types.Index) string {
	return fmt.Sprintf("alter table %s drop constraint %s", sanitizeTableName(tableName), pgx.Identifier{index.Name}.Sanitize())
}

// RemoveIndexStatement emits a guarded DROP INDEX for any index (unique or not).
// IF EXISTS is ALWAYS present so a re-run, a partial prior apply that already
// dropped the index, or a concurrent reconcile cannot wedge the plan with a 42704
// "index does not exist". DROP INDEX removes only the index, never table data.
func RemoveIndexStatement(tableName string, index *types.Index) string {
	return fmt.Sprintf("drop index if exists %s", pgx.Identifier{index.Name}.Sanitize())
}

func AddIndexStatement(tableName string, schemaIndex *schemasv1alpha4.PostgresqlTableIndex) string {
	unique := ""
	if schemaIndex.IsUnique {
		unique = "unique "
	}

	name := schemaIndex.Name
	if name == "" {
		name = types.GeneratePostgresqlIndexName(tableName, schemaIndex)
	}

	using := ""
	if method := strings.ToLower(strings.TrimSpace(schemaIndex.Type)); method != "" && method != "btree" {
		// The access method is an identifier (e.g. gin, hash, gist).
		using = fmt.Sprintf(" using %s", pgx.Identifier{method}.Sanitize())
	}

	statement := fmt.Sprintf("create %sindex %s on %s%s (%s)",
		unique,
		name,
		qualifyTableName(tableName),
		using,
		strings.Join(buildIndexElements(schemaIndex), ", "))

	if len(schemaIndex.With) > 0 {
		// Iterate keys in sorted order: Go map iteration is randomized and would
		// otherwise make multi-key WITH output non-deterministic.
		keys := make([]string, 0, len(schemaIndex.With))
		for key := range schemaIndex.With {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		withClauses := make([]string, 0, len(keys))
		for _, key := range keys {
			withClauses = append(withClauses, fmt.Sprintf("%s = %s", key, schemaIndex.With[key]))
		}
		statement += fmt.Sprintf(" with (%s)", strings.Join(withClauses, ", "))
	}

	if where := strings.TrimSpace(schemaIndex.Where); where != "" {
		// Raw SQL predicate (mirrors trigger condition handling).
		statement += fmt.Sprintf(" where %s", where)
	}

	return statement
}

// buildIndexElements assembles the parenthesised index element list. It appends,
// in order: plain columns (emitted unquoted to match Postgres' canonical
// reproduction and existing SchemaHero output), explicitly-ordered columns, then
// functional expressions wrapped in parentheses. Most indexes use exactly one of
// these forms.
func buildIndexElements(schemaIndex *schemasv1alpha4.PostgresqlTableIndex) []string {
	elements := make([]string, 0, len(schemaIndex.Columns)+len(schemaIndex.SortedColumns)+len(schemaIndex.Expressions))

	for _, column := range schemaIndex.Columns {
		elements = append(elements, column)
	}

	for _, sc := range schemaIndex.SortedColumns {
		element := sc.Column
		if dir := strings.ToUpper(strings.TrimSpace(sc.Sort)); dir == "DESC" {
			element += " desc"
		} else if dir == "ASC" {
			element += " asc"
		}
		if nulls := strings.ToUpper(strings.TrimSpace(sc.Nulls)); nulls == "FIRST" {
			element += " nulls first"
		} else if nulls == "LAST" {
			element += " nulls last"
		}
		elements = append(elements, element)
	}

	for _, expression := range schemaIndex.Expressions {
		// Expression bodies are operator-authored raw SQL, not identifiers, so
		// they must NOT be identifier-quoted; wrap in parens per the grammar.
		elements = append(elements, fmt.Sprintf("(%s)", strings.TrimSpace(expression)))
	}

	return elements
}

// isInlineFoldableUniqueIndex reports whether a unique index can be represented
// as an inline table-level UNIQUE constraint in CREATE TABLE. A UNIQUE
// constraint is always a total, btree, plain-column index, so an index that
// carries a method, partial predicate, expression, or column ordering cannot be
// folded inline and must instead be emitted as a standalone CREATE UNIQUE INDEX.
func isInlineFoldableUniqueIndex(schemaIndex *schemasv1alpha4.PostgresqlTableIndex) bool {
	if !schemaIndex.IsUnique {
		return false
	}
	if method := strings.ToLower(strings.TrimSpace(schemaIndex.Type)); method != "" && method != "btree" {
		return false
	}
	if strings.TrimSpace(schemaIndex.Where) != "" {
		return false
	}
	if len(schemaIndex.Expressions) > 0 {
		return false
	}
	if len(schemaIndex.SortedColumns) > 0 {
		return false
	}
	return true
}

func RenameIndexStatement(tableName string, index *types.Index, schemaIndex *schemasv1alpha4.PostgresqlTableIndex) string {
	return fmt.Sprintf("alter index %s rename to %s", pgx.Identifier{index.Name}.Sanitize(), pgx.Identifier{schemaIndex.Name}.Sanitize())
}
