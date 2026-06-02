package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"
	"github.com/schemahero/schemahero/pkg/database/types"
)

var (
	trueValue  = true
	falseValue = false
)

func (p *PostgresConnection) ListTables() ([]*types.Table, error) {
	tables := []*types.Table{}

	for _, schema := range p.schemas {
		query := "select table_name from information_schema.tables where table_catalog = $1 and table_schema = $2"

		rows, err := p.conn.Query(context.Background(), query, p.databaseName, schema)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("failed to list tables in schema %s", schema))
		}

		for rows.Next() {
			tableName := ""
			if err := rows.Scan(&tableName); err != nil {
				rows.Close()
				return nil, errors.Wrap(err, "failed to scan row")
			}

			qualifiedName := tableName
			if schema != "public" {
				qualifiedName = fmt.Sprintf("%s.%s", schema, tableName)
			}

			tables = append(tables, &types.Table{
				Name:   qualifiedName,
				Schema: schema,
			})
		}
		rows.Close()
	}

	return tables, nil
}

func (p *PostgresConnection) ListTableConstraints(databaseName string, tableName string) ([]string, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	query := `select constraint_name from information_schema.table_constraints
		where table_catalog = $1 and table_name = $2 and table_schema = $3`
	rows, err := p.conn.Query(context.Background(), query, databaseName, actualTableName, schema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list constraints")
	}
	defer rows.Close()

	constraints := []string{}
	for rows.Next() {
		var constraint string
		if err := rows.Scan(&constraint); err != nil {
			return nil, errors.Wrap(err, "failed to scan constraint")
		}

		constraints = append(constraints, constraint)
	}

	return constraints, nil
}

func (p *PostgresConnection) ListTableIndexes(databaseName string, tableName string) ([]*types.Index, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	qualifiedTableName := actualTableName
	if schema != "public" {
		qualifiedTableName = fmt.Sprintf("%s.%s", schema, actualTableName)
	}

	// started with this: https://stackoverflow.com/questions/6777456/list-all-index-names-column-names-and-its-table-name-of-a-postgresql-database
	// indpred (partial predicate) and the full canonical index definition are
	// captured so partial / ordered / expression indexes round-trip; the pretty
	// per-column array (3rd arg true) drops ordering, so ordering/expressions are
	// parsed from indexdef instead.
	query := `select
	i.relname as indname,
	am.amname as indam,
	idx.indisunique,
	array(
	  select pg_get_indexdef(idx.indexrelid, k + 1, true)
	  from generate_subscripts(idx.indkey, 1) as k
	  order by k
	) as indkey_names,
	 i.reloptions as reloptions,
	 pg_get_expr(idx.indpred, idx.indrelid) as indpred,
	 pg_get_indexdef(idx.indexrelid) as indexdef
	from pg_index as idx
	join pg_class as i on i.oid = idx.indexrelid
	join pg_am as am on i.relam = am.oid
	where idx.indrelid = $1::regclass
	and idx.indisprimary = false
	-- Exclude the backing index of an EXCLUDE constraint: it is owned by the
	-- constraint (dropped via DROP CONSTRAINT, which cascades it). If it leaked
	-- into the index diff, the existing-index sweep would emit "drop index <name>"
	-- which PostgreSQL rejects ("cannot drop index ... because constraint ...
	-- requires it"). indisexclusion is true exactly for such indexes (PG 9.1+).
	and idx.indisexclusion = false`
	rows, err := p.conn.Query(context.Background(), query, qualifiedTableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query indexes")
	}
	defer rows.Close()

	indexes := make([]*types.Index, 0)
	for rows.Next() {
		var index types.Index
		var method string
		var columns []string
		var reloptions map[string]string
		var indpred sql.NullString
		var indexdef string
		if err := rows.Scan(&index.Name, &method, &index.IsUnique, &columns, &reloptions, &indpred, &indexdef); err != nil {
			return nil, err
		}

		index.Columns = columns
		index.With = reloptions

		// Normalize the access method: store "" for the btree default so a spec
		// that omits the method matches an introspected btree index (no churn).
		if strings.ToLower(method) != "btree" {
			index.Type = method
		}

		if indpred.Valid {
			index.Where = indpred.String
		}

		// Parse ordering/expressions from the canonical definition. The pretty
		// per-column array above already gives bare names (kept in index.Columns);
		// only populate the richer fields when the index actually uses ordering or
		// an expression, so a plain index compares byte-identically to before.
		sortedColumns, expressions := parseIndexElements(indexdef)
		index.SortedColumns = sortedColumns
		index.Expressions = expressions

		indexes = append(indexes, &index)
	}

	return indexes, nil
}

func (p *PostgresConnection) ListTableForeignKeys(databaseName string, tableName string) ([]*types.ForeignKey, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	// Starting with a query here: https://stackoverflow.com/questions/1152260/postgres-sql-to-list-table-foreign-keys
	query := `select
	att2.attname as "child_column",
	cl.relname as "parent_table",
	att.attname as "parent_column",
  	rc.delete_rule,
	conname,
	ns2.nspname as "parent_schema"
    from
       (select
	    unnest(con1.conkey) as "parent",
	    unnest(con1.confkey) as "child",
	    con1.confrelid,
	    con1.conrelid,
	    con1.conname
	from
	    pg_class cl
	    join pg_namespace ns on cl.relnamespace = ns.oid
	    join pg_constraint con1 on con1.conrelid = cl.oid
	where
	    cl.relname = $1
	    and ns.nspname = $2
	    and con1.contype = 'f'
       ) con
       join pg_attribute att on
	   att.attrelid = con.confrelid and att.attnum = con.child
       join pg_class cl on
	   cl.oid = con.confrelid
       join pg_namespace ns2 on
       cl.relnamespace = ns2.oid
       join pg_attribute att2 on
	   att2.attrelid = con.conrelid and att2.attnum = con.parent
       join information_schema.referential_constraints rc on
       rc.constraint_name = conname`

	rows, err := p.conn.Query(context.Background(), query, actualTableName, schema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query foreign keys")
	}
	defer rows.Close()

	foreignKeys := make([]*types.ForeignKey, 0)
	for rows.Next() {
		var childColumn, parentColumn, parentTable, name, deleteRule, parentSchema string

		if err := rows.Scan(&childColumn, &parentTable, &parentColumn, &deleteRule, &name, &parentSchema); err != nil {
			return nil, err
		}

		qualifiedParentTable := parentTable
		if parentSchema != "public" {
			qualifiedParentTable = fmt.Sprintf("%s.%s", parentSchema, parentTable)
		}

		foreignKey := types.ForeignKey{
			Name:          name,
			ParentTable:   qualifiedParentTable,
			OnDelete:      deleteRule,
			ChildColumns:  []string{childColumn},
			ParentColumns: []string{parentColumn},
		}

		for _, foundFk := range foreignKeys {
			if foundFk.Name == name {
				foundFk.ChildColumns = append(foreignKey.ChildColumns, childColumn)
				foundFk.ParentColumns = append(foreignKey.ParentColumns, parentColumn)

				goto Appended
			}
		}

		foreignKeys = append(foreignKeys, &foreignKey)

	Appended:
	}

	return foreignKeys, nil
}

// ListTableCheckConstraints returns the table-level CHECK constraints declared
// on tableName. It introspects pg_constraint (contype='c') and renders each
// predicate via pg_get_constraintdef; the returned Expression is the inner
// boolean with the "CHECK (...)" wrapper stripped (see stripCheckWrapper).
//
// Modern PostgreSQL stores column NOT NULL in pg_attribute.attnotnull, NOT as a
// contype='c' row, so this query does not surface NOT NULL constraints. The
// integration suite exercises PG 14-18 to keep that guarantee honest.
func (p *PostgresConnection) ListTableCheckConstraints(databaseName string, tableName string) ([]*types.CheckConstraint, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	query := `select
	con.conname,
	pg_get_constraintdef(con.oid, true) as condef
from pg_constraint con
	join pg_class cl on cl.oid = con.conrelid
	join pg_namespace ns on ns.oid = cl.relnamespace
where con.contype = 'c'
	and con.conrelid <> 0
	and cl.relname = $1
	and ns.nspname = $2
order by con.conname`

	rows, err := p.conn.Query(context.Background(), query, actualTableName, schema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query check constraints")
	}
	defer rows.Close()

	checkConstraints := make([]*types.CheckConstraint, 0)
	for rows.Next() {
		var conname, condef string
		if err := rows.Scan(&conname, &condef); err != nil {
			return nil, err
		}

		checkConstraints = append(checkConstraints, &types.CheckConstraint{
			Name:       conname,
			Expression: stripCheckWrapper(condef),
		})
	}

	return checkConstraints, nil
}

// ListTableExclusionConstraints returns the table-level EXCLUDE constraints
// declared on tableName. It introspects pg_constraint (contype='x') and parses
// the canonical pg_get_constraintdef rendering (the most robust source of truth
// for the access method, ordered element/operator pairs, and partial predicate).
// The constraint's backing index is deliberately NOT returned by ListTableIndexes
// (filtered via indisexclusion=false) so the index diff never tries to DROP INDEX
// it; an EXCLUDE is dropped via DROP CONSTRAINT, which cascades the index.
func (p *PostgresConnection) ListTableExclusionConstraints(databaseName string, tableName string) ([]*types.ExclusionConstraint, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	query := `select
	con.conname,
	pg_get_constraintdef(con.oid, true) as condef
from pg_constraint con
	join pg_class cl on cl.oid = con.conrelid
	join pg_namespace ns on ns.oid = cl.relnamespace
where con.contype = 'x'
	and cl.relname = $1
	and ns.nspname = $2
order by con.conname`

	rows, err := p.conn.Query(context.Background(), query, actualTableName, schema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query exclusion constraints")
	}
	defer rows.Close()

	exclusionConstraints := make([]*types.ExclusionConstraint, 0)
	for rows.Next() {
		var conname, condef string
		if err := rows.Scan(&conname, &condef); err != nil {
			return nil, err
		}

		parsed := parseExclusionConstraintDef(condef)
		parsed.Name = conname
		exclusionConstraints = append(exclusionConstraints, &parsed)
	}

	return exclusionConstraints, nil
}

// pgTrigger is the introspected form of a user trigger: its name plus the
// canonical definition reported by pg_get_triggerdef. It is plugin-local (not
// exported, not part of pkg/database/types and not on the RPC interface) because
// BuildTriggerStatements calls ListTableTriggers in-process with the concrete
// connection — the List* interface methods exist only for the legacy
// cross-process planner. Definition is stored raw; normalization happens in the
// comparator so the raw value stays available for debugging.
type pgTrigger struct {
	Name       string
	Definition string
}

// ListTableTriggers returns the user-defined triggers on tableName.
//
// The tgisinternal=false filter is load-bearing: it drops BOTH system-internal
// triggers AND the auto-generated RI/constraint triggers backing foreign keys.
// Without it, every FK's internal triggers would surface as unmanaged triggers
// and the diff would emit spurious DROPs of system triggers (breaking
// referential integrity). User-declared CREATE CONSTRAINT TRIGGERs are NOT
// internal and correctly still appear (they are user-managed).
//
// pg_get_triggerdef(oid, true) renders the complete, canonical
// "CREATE TRIGGER ... ON schema.table ... EXECUTE FUNCTION fn(...)" with
// fully schema-qualified identifiers (PG12+ always uses EXECUTE FUNCTION). It is
// the safest single source of truth for comparison and avoids reconstructing the
// definition from the tgtype bitmask.
func (p *PostgresConnection) ListTableTriggers(tableName string) ([]pgTrigger, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	query := `select t.tgname,
		pg_get_triggerdef(t.oid, true) as def
	from pg_trigger t
		join pg_class c on c.oid = t.tgrelid
		join pg_namespace n on n.oid = c.relnamespace
	where c.relname = $1
		and n.nspname = $2
		and t.tgisinternal = false
	order by t.tgname`

	rows, err := p.conn.Query(context.Background(), query, actualTableName, schema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query triggers")
	}
	defer rows.Close()

	triggers := make([]pgTrigger, 0)
	for rows.Next() {
		var tgname, def string
		if err := rows.Scan(&tgname, &def); err != nil {
			return nil, err
		}

		triggers = append(triggers, pgTrigger{
			Name:       tgname,
			Definition: def,
		})
	}

	return triggers, nil
}

func (p *PostgresConnection) GetTablePrimaryKey(tableName string) (*types.KeyConstraint, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	query := `SELECT tc.constraint_name, kcu.column_name
FROM information_schema.table_constraints  AS tc
JOIN information_schema.key_column_usage   AS kcu
  ON  kcu.constraint_catalog  = tc.constraint_catalog
  AND kcu.constraint_schema   = tc.constraint_schema
  AND kcu.constraint_name     = tc.constraint_name
WHERE tc.constraint_type = 'PRIMARY KEY'
  AND tc.table_name      = $1
  AND tc.table_schema    = $2
  AND tc.constraint_catalog = $3
ORDER BY kcu.ordinal_position`

	rows, err := p.conn.Query(context.Background(), query, actualTableName, schema, p.databaseName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query primary keys")
	}
	defer rows.Close()

	var hasKey bool

	key := types.KeyConstraint{
		IsPrimary: true,
	}
	for rows.Next() {
		hasKey = true

		var constraintName, columnName string

		if err := rows.Scan(&constraintName, &columnName); err != nil {
			return nil, err
		}

		key.Name = constraintName
		key.Columns = append(key.Columns, columnName)
	}
	if !hasKey {
		return nil, nil
	}

	return &key, nil
}

func (p *PostgresConnection) GetTableSchema(tableName string) ([]*types.Column, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	query := "select column_name, data_type, character_maximum_length, column_default, is_nullable from information_schema.columns where table_name = $1 and table_schema = $2 and table_catalog = $3"

	rows, err := p.conn.Query(context.Background(), query, actualTableName, schema, p.databaseName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query table schema")
	}
	defer rows.Close()

	columns := make([]*types.Column, 0)
	for rows.Next() {
		column := types.Column{}

		var maxLength sql.NullInt64
		var isNullable string
		var columnDefault sql.NullString

		if err := rows.Scan(&column.Name, &column.DataType, &maxLength, &columnDefault, &isNullable); err != nil {
			return nil, err
		}

		if isNullable == "NO" {
			column.Constraints = &types.ColumnConstraints{
				NotNull: &trueValue,
			}
		} else {
			column.Constraints = &types.ColumnConstraints{
				NotNull: &falseValue,
			}
		}

		if columnDefault.Valid {
			value := stripOIDClass(columnDefault.String)
			column.ColumnDefault = &value
		}

		if maxLength.Valid {
			column.DataType = fmt.Sprintf("%s (%d)", column.DataType, maxLength.Int64)
		}

		columns = append(columns, &column)
	}

	return columns, nil
}

// GetTableRowSecurity returns the table's row-level security flags from
// pg_class (relrowsecurity = ENABLE state, relforcerowsecurity = FORCE state).
// Both are non-null bool columns. The schema is resolved the same way as the
// other introspectors: from a "schema." prefix on tableName, else the
// connection's default schema.
//
// If the table is not found (e.g. it does not exist yet), it returns the zero
// RowLevelSecurity{} (both false) and no error — the caller (CheckIfTableExists)
// already gates on existence, so this is only reached for an existing table.
func (p *PostgresConnection) GetTableRowSecurity(tableName string) (*types.RowLevelSecurity, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	query := `select c.relrowsecurity, c.relforcerowsecurity
from pg_class c
	join pg_namespace n on n.oid = c.relnamespace
where c.relname = $1
	and n.nspname = $2`

	row := p.conn.QueryRow(context.Background(), query, actualTableName, schema)

	var enabled, forced bool
	if err := row.Scan(&enabled, &forced); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &types.RowLevelSecurity{}, nil
		}
		return nil, errors.Wrap(err, "failed to scan row level security flags")
	}

	return &types.RowLevelSecurity{Enabled: enabled, Forced: forced}, nil
}

// ListTablePolicies returns the row-level security policies on tableName from
// pg_policy. polcmd is a single char ('*' ALL, 'r' SELECT, 'a' INSERT, 'w'
// UPDATE, 'd' DELETE) and is mapped to a Command string. polpermissive is true
// for PERMISSIVE. The USING / WITH CHECK expressions are rendered canonically by
// pg_get_expr (NULL when absent). The applied-to roles are resolved from the
// polroles oid[] to role names; the pseudo-role PUBLIC (OID 0) has no pg_authid
// row, so the array subselect drops it and COALESCE maps "no rows" -> ['public']
// — matching how the desired-side converter normalizes empty roles.
func (p *PostgresConnection) ListTablePolicies(tableName string) ([]*types.Policy, error) {
	schema := p.schema // Default to connection schema
	actualTableName := tableName

	if strings.Contains(tableName, ".") {
		parts := strings.SplitN(tableName, ".", 2)
		schema = parts[0]
		actualTableName = parts[1]
	}

	query := `select
	pol.polname,
	pol.polcmd,
	pol.polpermissive,
	pg_get_expr(pol.polqual, pol.polrelid) as using_expr,
	pg_get_expr(pol.polwithcheck, pol.polrelid) as with_check_expr,
	coalesce(
		(select array_agg(a.rolname::text order by a.rolname)
		 from pg_authid a
		 where a.oid = any(pol.polroles)),
		array['public']::text[]
	) as roles
from pg_policy pol
	join pg_class c on c.oid = pol.polrelid
	join pg_namespace n on n.oid = c.relnamespace
where c.relname = $1
	and n.nspname = $2
order by pol.polname`

	rows, err := p.conn.Query(context.Background(), query, actualTableName, schema)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query policies")
	}
	defer rows.Close()

	policies := make([]*types.Policy, 0)
	for rows.Next() {
		var name string
		// pg_policy.polcmd is the internal "char" type (a single byte), which pgx
		// surfaces as int32 (e.g. 42 for '*'), NOT a Go string. Scan it as a rune
		// and convert before mapping to a Command.
		var polcmd int32
		var permissive bool
		var usingExpr, withCheckExpr sql.NullString
		var roles []string

		if err := rows.Scan(&name, &polcmd, &permissive, &usingExpr, &withCheckExpr, &roles); err != nil {
			return nil, err
		}

		policy := types.Policy{
			Name:       name,
			Command:    types.PolicyCommandFromCatalogChar(string(rune(polcmd))),
			Permissive: permissive,
			Roles:      roles,
		}

		if usingExpr.Valid {
			value := usingExpr.String
			policy.Using = &value
		}
		if withCheckExpr.Valid {
			value := withCheckExpr.String
			policy.WithCheck = &value
		}

		policies = append(policies, &policy)
	}

	return policies, nil
}
