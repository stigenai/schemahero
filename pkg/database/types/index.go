package types

import (
	"fmt"
	"strings"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// IndexColumn is a single index column with optional operator class and
// ordering. It is the dialect-neutral mirror of the apis
// PostgresqlTableIndexColumn.
type IndexColumn struct {
	Column string
	// OpClass is the operator class name, e.g. "jsonb_path_ops". Empty means
	// the type's default opclass and is omitted from DDL. Compared
	// case-insensitively; see sortedColumnsEqual.
	OpClass string
	Sort    string // ASC (default) | DESC
	Nulls   string // FIRST | LAST
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

// normalizeSQLFragment canonicalizes a raw-SQL fragment (partial-index predicate
// or functional-index expression) for comparison via the shared CanonicalizeSQLExpr:
// lowercase, strip "::type" casts, drop all parentheses, collapse whitespace. This
// is the SAME canonicalization the CHECK comparator uses, and it is load-bearing:
// PostgreSQL stores/renders these fragments in canonical form (pg_get_expr turns
// "lower(email)" into "lower((email)::text)", and a comparison to an empty string
// literal gains "(phone)::text" / "::text" casts), so a fragment authored in
// natural form would otherwise differ on every plan and force a needless index
// drop+recreate (a heavy lock + rebuild, plus a momentary uniqueness-guard gap for
// a unique index).
func normalizeSQLFragment(s string) string {
	return CanonicalizeSQLExpr(s)
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
		// Operator class comparison is case-insensitive. An empty/omitted opclass
		// on both sides is equal (both use the type's default). An explicit opclass
		// on one side and none on the other is a real difference: the stored index
		// was created with a specific opclass (e.g. jsonb_path_ops for a GIN index)
		// and must be dropped+recreated if the spec changes it.
		if strings.ToLower(strings.TrimSpace(a[i].OpClass)) != strings.ToLower(strings.TrimSpace(b[i].OpClass)) {
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
			Column:  sc.Column,
			OpClass: sc.OpClass,
			Sort:    sc.Sort,
			Nulls:   sc.Nulls,
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
			Column:  sc.Column,
			OpClass: sc.OpClass,
			Sort:    sc.Sort,
			Nulls:   sc.Nulls,
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
	return capPostgresIdentifier(fmt.Sprintf("idx_%s_%s", bareTableName(tableName), strings.Join(schemaIndex.Columns, "_")))
}

func GenerateSqliteIndexName(tableName string, schemaIndex *schemasv1alpha4.SqliteTableIndex) string {
	return fmt.Sprintf("idx_%s_%s", tableName, strings.Join(schemaIndex.Columns, "_"))
}

func GenerateRqliteIndexName(tableName string, schemaIndex *schemasv1alpha4.RqliteTableIndex) string {
	return fmt.Sprintf("idx_%s_%s", tableName, strings.Join(schemaIndex.Columns, "_"))
}
