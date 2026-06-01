package types

import (
	"fmt"
	"strings"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// IndexColumn is a single index column with optional ordering. It is the
// dialect-neutral mirror of the apis PostgresqlTableIndexColumn.
type IndexColumn struct {
	Column string
	Sort   string // ASC (default) | DESC
	Nulls  string // FIRST | LAST
}

type Index struct {
	Columns  []string
	Name     string
	IsUnique bool
	With     map[string]string
	// Type is the index access method (e.g. gin, hash). Empty means the btree default.
	Type string
	// Where is a partial-index predicate (raw SQL, without the WHERE keyword).
	Where string
	// Expressions are functional-index column expressions (raw SQL).
	Expressions []string
	// SortedColumns carries ordered (ASC/DESC/NULLS) index columns.
	SortedColumns []IndexColumn
}

// normalizeIndexMethod maps the empty string and "btree" (case-insensitively)
// to a canonical "btree" so that an omitted method matches an introspected
// btree index and does not churn.
func normalizeIndexMethod(method string) string {
	m := strings.ToLower(strings.TrimSpace(method))
	if m == "" {
		return "btree"
	}
	return m
}

// normalizeSQLFragment lower-cases and collapses internal whitespace so two
// equivalent raw-SQL fragments (predicates, expressions) compare equal even
// when spacing/case differs. This is best-effort: a predicate whose text is not
// already in Postgres' canonical form may still re-churn (a guarded, non
// data-losing drop+recreate), matching the accepted behavior for column
// defaults elsewhere in this plugin.
func normalizeSQLFragment(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func (idx *Index) Equals(other *Index) bool {
	if idx.Name != other.Name {
		return false
	}

	if idx.IsUnique != other.IsUnique {
		return false
	}

	if normalizeIndexMethod(idx.Type) != normalizeIndexMethod(other.Type) {
		return false
	}

	if normalizeSQLFragment(idx.Where) != normalizeSQLFragment(other.Where) {
		return false
	}

	// When either side carries ordering or expression columns, those define the
	// index element list and the order is significant; compare them positionally
	// and skip the legacy order-insensitive Columns set match. When neither side
	// uses the richer fields, fall back to the historical Columns set compare so
	// existing (non-extended) indexes behave byte-identically to before.
	if len(idx.SortedColumns) > 0 || len(other.SortedColumns) > 0 || len(idx.Expressions) > 0 || len(other.Expressions) > 0 {
		if !sortedColumnsEqual(idx.SortedColumns, other.SortedColumns) {
			return false
		}
		if !expressionsEqual(idx.Expressions, other.Expressions) {
			return false
		}
		return true
	}

	if len(idx.Columns) != len(other.Columns) {
		return false
	}

	for _, otherColumn := range other.Columns {
		for _, col := range idx.Columns {
			if col == otherColumn {
				goto NextColumn
			}
		}

		return false

	NextColumn:
	}

	return true
}

func sortedColumnsEqual(a, b []IndexColumn) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// An omitted sort direction is ASC; normalize so "id" == "id ASC".
		aSort := strings.ToUpper(strings.TrimSpace(a[i].Sort))
		bSort := strings.ToUpper(strings.TrimSpace(b[i].Sort))
		if aSort == "" {
			aSort = "ASC"
		}
		if bSort == "" {
			bSort = "ASC"
		}
		if a[i].Column != b[i].Column || aSort != bSort {
			return false
		}
		if strings.ToUpper(strings.TrimSpace(a[i].Nulls)) != strings.ToUpper(strings.TrimSpace(b[i].Nulls)) {
			return false
		}
	}
	return true
}

func expressionsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if normalizeSQLFragment(a[i]) != normalizeSQLFragment(b[i]) {
			return false
		}
	}
	return true
}

func IndexToMysqlSchemaIndex(index *Index) *schemasv1alpha4.MysqlTableIndex {
	schemaIndex := schemasv1alpha4.MysqlTableIndex{
		Columns:  index.Columns,
		Name:     index.Name,
		IsUnique: index.IsUnique,
	}

	return &schemaIndex
}

func IndexToPostgresqlSchemaIndex(index *Index) *schemasv1alpha4.PostgresqlTableIndex {
	schemaIndex := schemasv1alpha4.PostgresqlTableIndex{
		Columns:     index.Columns,
		Name:        index.Name,
		IsUnique:    index.IsUnique,
		Type:        index.Type,
		With:        index.With,
		Where:       index.Where,
		Expressions: index.Expressions,
	}

	for _, sc := range index.SortedColumns {
		schemaIndex.SortedColumns = append(schemaIndex.SortedColumns, &schemasv1alpha4.PostgresqlTableIndexColumn{
			Column: sc.Column,
			Sort:   sc.Sort,
			Nulls:  sc.Nulls,
		})
	}

	return &schemaIndex
}

func IndexToRqliteSchemaIndex(index *Index) *schemasv1alpha4.RqliteTableIndex {
	schemaIndex := schemasv1alpha4.RqliteTableIndex{
		Columns:  index.Columns,
		Name:     index.Name,
		IsUnique: index.IsUnique,
	}

	return &schemaIndex
}

func IndexToSqliteSchemaIndex(index *Index) *schemasv1alpha4.SqliteTableIndex {
	schemaIndex := schemasv1alpha4.SqliteTableIndex{
		Columns:  index.Columns,
		Name:     index.Name,
		IsUnique: index.IsUnique,
	}

	return &schemaIndex
}

func MysqlSchemaIndexToIndex(schemaIndex *schemasv1alpha4.MysqlTableIndex) *Index {
	index := Index{
		Columns:  schemaIndex.Columns,
		Name:     schemaIndex.Name,
		IsUnique: schemaIndex.IsUnique,
	}

	return &index
}

func PostgresqlSchemaIndexToIndex(schemaIndex *schemasv1alpha4.PostgresqlTableIndex) *Index {
	index := Index{
		Columns:     schemaIndex.Columns,
		Name:        schemaIndex.Name,
		IsUnique:    schemaIndex.IsUnique,
		Type:        schemaIndex.Type,
		With:        schemaIndex.With,
		Where:       schemaIndex.Where,
		Expressions: schemaIndex.Expressions,
	}

	for _, sc := range schemaIndex.SortedColumns {
		index.SortedColumns = append(index.SortedColumns, IndexColumn{
			Column: sc.Column,
			Sort:   sc.Sort,
			Nulls:  sc.Nulls,
		})
	}

	// Keep Columns populated (bare names) for back-compat and name generation
	// when only SortedColumns were provided.
	if len(index.Columns) == 0 && len(index.SortedColumns) > 0 {
		for _, sc := range index.SortedColumns {
			index.Columns = append(index.Columns, sc.Column)
		}
	}

	return &index
}

func SqliteSchemaIndexToIndex(schemaIndex *schemasv1alpha4.SqliteTableIndex) *Index {
	index := Index{
		Columns:  schemaIndex.Columns,
		Name:     schemaIndex.Name,
		IsUnique: schemaIndex.IsUnique,
	}

	return &index
}

func RqliteSchemaIndexToIndex(schemaIndex *schemasv1alpha4.RqliteTableIndex) *Index {
	index := Index{
		Columns:  schemaIndex.Columns,
		Name:     schemaIndex.Name,
		IsUnique: schemaIndex.IsUnique,
	}

	return &index
}

func GenerateMysqlIndexName(tableName string, schemaIndex *schemasv1alpha4.MysqlTableIndex) string {
	indexName := fmt.Sprintf("idx_%s_%s", tableName, strings.Join(schemaIndex.Columns, "_"))
	if len(indexName) > 64 {
		indexName = indexName[:64]
	}
	return indexName
}

func GeneratePostgresqlIndexName(tableName string, schemaIndex *schemasv1alpha4.PostgresqlTableIndex) string {
	return fmt.Sprintf("idx_%s_%s", bareTableName(tableName), strings.Join(schemaIndex.Columns, "_"))
}

func GenerateSqliteIndexName(tableName string, schemaIndex *schemasv1alpha4.SqliteTableIndex) string {
	return fmt.Sprintf("idx_%s_%s", tableName, strings.Join(schemaIndex.Columns, "_"))
}

func GenerateRqliteIndexName(tableName string, schemaIndex *schemasv1alpha4.RqliteTableIndex) string {
	return fmt.Sprintf("idx_%s_%s", tableName, strings.Join(schemaIndex.Columns, "_"))
}
