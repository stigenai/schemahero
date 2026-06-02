package postgres

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v4"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"
)

// RemoveExclusionConstraintStatement emits a guarded ALTER TABLE ... DROP
// CONSTRAINT for an introspected EXCLUDE. It is ALWAYS a DROP CONSTRAINT (never a
// DROP INDEX): an EXCLUDE owns a backing index, and DROP CONSTRAINT cascades that
// index, whereas DROP INDEX would be rejected ("cannot drop index ... because
// constraint ... requires it"). The table is qualified via qualifyTableName and
// the constraint name is always quoted via pgx.Identifier.Sanitize.
func RemoveExclusionConstraintStatement(tableName string, c *types.ExclusionConstraint) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf(
		"alter table %s drop constraint %s",
		qualifyTableName(tableName),
		pgx.Identifier{c.Name}.Sanitize(),
	)
}

// AddExclusionConstraintStatement emits ALTER TABLE ... ADD CONSTRAINT <name>
// EXCLUDE USING <method> (<element> WITH <op>, ...) [WITH (params)] [WHERE (pred)].
func AddExclusionConstraintStatement(tableName string, c *schemasv1alpha4.PostgresqlTableExclusionConstraint) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf(
		"alter table %s add %s",
		qualifyTableName(tableName),
		exclusionConstraintClause(tableName, c),
	)
}

// exclusionConstraintClause renders the inline "constraint <name> exclude using
// <method> (...)" fragment. It is used both by AddExclusionConstraintStatement
// and appended into a CREATE TABLE column list (mirroring how FK/CHECK clauses
// are emitted inline so a brand-new table needs no separate ADD pass).
//
// Quoting rules (these are exactly the over-quoting bug class the fork has
// already shipped twice):
//   - constraint name: identifier-quoted (it is bare via the name generator, so
//     it can never carry a dot).
//   - access method: identifier-quoted (gist/gin/...).
//   - column element: identifier-quoted.
//   - expression element: wrapped in parens and emitted VERBATIM (operator-
//     authored SQL; quoting it would break it) — same philosophy as index
//     expressions and WHERE predicates.
//   - operator: raw token, never quoted (it is a PostgreSQL operator).
//   - WHERE predicate and storage params: emitted verbatim.
func exclusionConstraintClause(tableName string, c *schemasv1alpha4.PostgresqlTableExclusionConstraint) string {
	using := strings.ToLower(strings.TrimSpace(c.Using))
	if using == "" {
		using = "gist"
	}

	items := make([]string, 0, len(c.Items))
	for _, item := range c.Items {
		var element string
		if item.Expression != "" {
			element = fmt.Sprintf("(%s)", strings.TrimSpace(item.Expression))
		} else {
			element = pgx.Identifier{item.Column}.Sanitize()
		}
		items = append(items, fmt.Sprintf("%s with %s", element, strings.TrimSpace(item.Operator)))
	}

	clause := fmt.Sprintf(
		"constraint %s exclude using %s (%s)",
		pgx.Identifier{types.GeneratePostgresqlExclusionConstraintName(tableName, c)}.Sanitize(),
		pgx.Identifier{using}.Sanitize(),
		strings.Join(items, ", "),
	)

	if len(c.With) > 0 {
		// Iterate keys in sorted order: Go map iteration is randomized and would
		// otherwise make multi-key WITH output non-deterministic.
		keys := make([]string, 0, len(c.With))
		for key := range c.With {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		withClauses := make([]string, 0, len(keys))
		for _, key := range keys {
			withClauses = append(withClauses, fmt.Sprintf("%s = %s", key, c.With[key]))
		}
		clause += fmt.Sprintf(" with (%s)", strings.Join(withClauses, ", "))
	}

	if where := strings.TrimSpace(c.Where); where != "" {
		clause += fmt.Sprintf(" where (%s)", where)
	}

	return clause
}

// parseExclusionConstraintDef parses a canonical pg_get_constraintdef rendering
// of an EXCLUDE constraint into its access method, ordered items, and partial
// predicate, e.g.
//
//	EXCLUDE USING gist (room_id WITH =, during WITH &&) WHERE (room_id IS NOT NULL)
//
// yields using="gist", items=[{room_id,=},{during,&&}], where="room_id IS NOT NULL".
// The element of each item is stored in Expression (the introspected form is the
// rendered element text, whether a bare column or an expression); Equals compares
// element-vs-element so this round-trips against a desired spec that set Column.
func parseExclusionConstraintDef(condef string) types.ExclusionConstraint {
	result := types.ExclusionConstraint{}

	s := strings.TrimSpace(condef)

	// Access method: the token following "USING".
	if idx := indexOfWordCI(s, "using"); idx >= 0 {
		rest := strings.TrimSpace(s[idx+len("using"):])
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			result.Using = strings.ToLower(fields[0])
		}
	}

	// Element list: the first balanced "(...)" group.
	elementList := extractIndexElementList(s)
	for _, raw := range splitTopLevelCommas(elementList) {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		// Each element is "<element> WITH <operator>"; WITH is matched
		// case-insensitively and only at top level (the element may itself be an
		// expression containing parentheses, but never a top-level WITH).
		element, operator := splitItemOnWith(item)
		result.Items = append(result.Items, types.ExclusionConstraintItem{
			Expression: strings.TrimSpace(element),
			Operator:   strings.TrimSpace(operator),
		})
	}

	// Partial predicate: trailing "WHERE (...)".
	if idx := indexOfWordCI(s, "where"); idx >= 0 {
		pred := strings.TrimSpace(s[idx+len("where"):])
		if strings.HasPrefix(pred, "(") && hasBalancedOuterParens(pred) {
			pred = strings.TrimSpace(pred[1 : len(pred)-1])
		}
		result.Where = pred
	}

	return result
}

// splitItemOnWith splits an EXCLUDE element on the top-level " WITH " keyword
// (case-insensitive), returning the element text and the operator token. Parens
// nesting is respected so a WITH appearing inside an expression element is not
// mistaken for the separator.
func splitItemOnWith(item string) (element string, operator string) {
	depth := 0
	for i := 0; i < len(item); i++ {
		switch item[i] {
		case '(':
			depth++
		case ')':
			depth--
		case 'w', 'W':
			if depth != 0 {
				continue
			}
			// Require word boundaries around "with".
			if i+4 > len(item) {
				continue
			}
			if !strings.EqualFold(item[i:i+4], "with") {
				continue
			}
			beforeOK := i == 0 || isSpace(item[i-1])
			afterOK := i+4 == len(item) || isSpace(item[i+4])
			if beforeOK && afterOK {
				return item[:i], item[i+4:]
			}
		}
	}
	// No WITH found (malformed); treat the whole thing as the element.
	return item, ""
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// indexOfWordCI returns the byte index of the first case-insensitive, whitespace-
// delimited occurrence of word in s, or -1. It is used to locate the USING and
// WHERE keywords in a constraint definition without matching them inside an
// identifier or string.
func indexOfWordCI(s string, word string) int {
	n := len(word)
	for i := 0; i+n <= len(s); i++ {
		if !strings.EqualFold(s[i:i+n], word) {
			continue
		}
		beforeOK := i == 0 || isSpace(s[i-1])
		afterOK := i+n == len(s) || isSpace(s[i+n]) || s[i+n] == '('
		if beforeOK && afterOK {
			return i
		}
	}
	return -1
}

// BuildExclusionConstraintStatements computes the ADD/DROP statements that
// reconcile the table's current EXCLUDE constraints to
// postgresTableSchema.ExclusionConstraints. It mirrors BuildCheckConstraintStatements /
// BuildForeignKeyStatements exactly: a desired pass that adds new constraints and
// drops-then-re-adds a same-named constraint whose definition changed, then an
// existing pass that drops any current EXCLUDE absent from the desired set.
//
// Drop guarding: a current EXCLUDE is dropped ONLY when (a) a desired constraint
// of the same generated name has a changed definition (drop emitted immediately
// before its re-add), or (b) it exists on the table but is absent from the
// desired set AND has not already been dropped in the desired pass. There is no
// unconditional/blanket DROP, and the removal is ALWAYS DROP CONSTRAINT (which
// cascades the backing index), never DROP INDEX. The desired list is therefore
// AUTHORITATIVE for table-level EXCLUDE constraints (identical to how foreignKeys,
// checks, and indexes already behave in this plugin).
func BuildExclusionConstraintStatements(p *PostgresConnection, tableName string, postgresTableSchema *schemasv1alpha4.PostgresqlTableSchema) ([]string, error) {
	statements := []string{}
	droppedNames := []string{}

	currentExclusions, err := p.ListTableExclusionConstraints(p.databaseName, tableName)
	if err != nil {
		return nil, err
	}

	// Desired pass: add new constraints; drop+re-add a changed one (matched by name).
	for _, desired := range postgresTableSchema.ExclusionConstraints {
		desiredType := types.PostgresqlSchemaExclusionConstraintToExclusionConstraint(desired)
		name := types.GeneratePostgresqlExclusionConstraintName(tableName, desired)

		var matched *types.ExclusionConstraint
		for _, current := range currentExclusions {
			if current.Name == name {
				matched = current
				break
			}
		}

		if matched != nil && matched.Equals(desiredType) {
			// A same-named, equivalent constraint already exists: no-op (idempotent).
			continue
		}

		if matched != nil {
			// Same name, different definition. PostgreSQL has no ALTER for an EXCLUDE
			// definition, so drop then re-add. Record the name so the existing pass
			// below does not drop it a second time.
			statements = append(statements, RemoveExclusionConstraintStatement(tableName, matched))
			droppedNames = append(droppedNames, matched.Name)
		}

		statements = append(statements, AddExclusionConstraintStatement(tableName, desired))
	}

	// Existing pass: drop any current EXCLUDE not present in the desired set.
	for _, current := range currentExclusions {
		isDesired := false
		for _, desired := range postgresTableSchema.ExclusionConstraints {
			if types.GeneratePostgresqlExclusionConstraintName(tableName, desired) == current.Name {
				isDesired = true
				break
			}
		}
		if isDesired {
			continue
		}

		alreadyDropped := false
		for _, dropped := range droppedNames {
			if dropped == current.Name {
				alreadyDropped = true
				break
			}
		}
		if alreadyDropped {
			continue
		}

		statements = append(statements, RemoveExclusionConstraintStatement(tableName, current))
	}

	return statements, nil
}
