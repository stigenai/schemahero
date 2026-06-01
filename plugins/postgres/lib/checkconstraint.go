package postgres

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"
)

// RemoveCheckConstraintStatement emits a guarded ALTER TABLE ... DROP CONSTRAINT
// for an introspected CHECK. The table is qualified via qualifyTableName and the
// constraint name is always quoted via pgx.Identifier.Sanitize.
func RemoveCheckConstraintStatement(tableName string, c *types.CheckConstraint) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf(
		"alter table %s drop constraint %s",
		qualifyTableName(tableName),
		pgx.Identifier{c.Name}.Sanitize(),
	)
}

// AddCheckConstraintStatement emits ALTER TABLE ... ADD CONSTRAINT <name> CHECK
// (<expr>). The name is quoted; the expression is emitted VERBATIM because it is
// already SQL authored by the user (exactly like an FK references clause) and
// must never be quoted/escaped (that was the class of bug behind the
// column-default over-quoting regression).
func AddCheckConstraintStatement(tableName string, c *schemasv1alpha4.PostgresqlTableCheckConstraint) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf(
		"alter table %s add constraint %s check (%s)",
		qualifyTableName(tableName),
		pgx.Identifier{types.GeneratePostgresqlCheckName(tableName, c)}.Sanitize(),
		c.Expression,
	)
}

// checkConstraintClause renders the inline "constraint <name> check (<expr>)"
// fragment appended into a CREATE TABLE column list, mirroring how FK clauses
// are emitted inline so a brand-new table needs no separate ADD pass.
func checkConstraintClause(tableName string, c *schemasv1alpha4.PostgresqlTableCheckConstraint) string {
	return fmt.Sprintf(
		"constraint %s check (%s)",
		pgx.Identifier{types.GeneratePostgresqlCheckName(tableName, c)}.Sanitize(),
		c.Expression,
	)
}

// stripCheckWrapper recovers the inner boolean from a pg_get_constraintdef
// rendering, e.g. "CHECK ((age >= 0))" -> "(age >= 0)". It trims the leading
// CHECK keyword (case-insensitively, defensively) and removes exactly one
// balanced outer parenthesis pair if present. It deliberately does NOT fully
// normalize the expression; normalization for comparison lives in
// checkExprEquivalent so the read and the compare stay co-located and symmetric.
func stripCheckWrapper(condef string) string {
	s := strings.TrimSpace(condef)

	// Strip the leading CHECK keyword regardless of case.
	if len(s) >= 5 && strings.EqualFold(s[:5], "check") {
		s = strings.TrimSpace(s[5:])
	}

	// Remove exactly one balanced outer paren pair, if the whole thing is wrapped.
	if strings.HasPrefix(s, "(") && hasBalancedOuterParens(s) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}

	return s
}

// hasBalancedOuterParens reports whether s begins with '(' and that opening
// paren is closed only by the final character, i.e. the outermost pair wraps the
// entire string. This guards against stripping a pair that does not actually
// enclose everything, e.g. "(a) and (b)".
func hasBalancedOuterParens(s string) bool {
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				// The first opening paren closed here; it wraps everything only
				// if that is the final character.
				return i == len(s)-1
			}
		}
	}
	return false
}

// checkExprEquivalent conservatively reports whether a user-authored CHECK
// expression and an introspected one are equivalent. PostgreSQL re-parenthesizes,
// lowercases operators, and adds ::type casts on round-trip, so a byte compare
// would force a destructive DROP+ADD on every plan (re-validating a CHECK locks
// and scans the whole table). Both sides are normalized by: lowercasing,
// stripping all parentheses, removing ::type casts, and collapsing whitespace,
// then compared. When normalization is ambiguous we prefer to treat the two as
// EQUAL (a missed no-op alter is far cheaper than a spurious destructive churn
// against production).
func checkExprEquivalent(a, b string) bool {
	return normalizeCheckExpr(a) == normalizeCheckExpr(b)
}

// normalizeCheckExpr lowercases, removes ::type casts and all parentheses, and
// collapses whitespace so two expressions that differ only by PostgreSQL's
// canonicalization compare equal.
func normalizeCheckExpr(s string) string {
	s = strings.ToLower(s)
	s = stripTypeCasts(s)

	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		switch r {
		case '(', ')':
			// drop parens entirely
			continue
		case ' ', '\t', '\n', '\r':
			pendingSpace = true
			continue
		default:
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// stripTypeCasts removes PostgreSQL "::type" cast suffixes, including ones with
// a parenthesized length/modifier such as "::character varying" or
// "::numeric(10,2)". It scans for "::" and drops the following type token (an
// identifier run, optional whitespace-separated words like "character varying",
// and an optional "( ... )" modifier).
func stripTypeCasts(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == ':' && s[i+1] == ':' {
			i += 2
			i = skipTypeToken(s, i)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// skipTypeToken advances past a type name beginning at index i: a run of
// identifier characters, optionally followed by additional whitespace-separated
// identifier words (e.g. "character varying", "double precision") and an
// optional balanced "( ... )" modifier. It returns the index just past the type.
func skipTypeToken(s string, i int) int {
	consumeWord := func(j int) int {
		for j < len(s) {
			c := s[j]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
				j++
				continue
			}
			break
		}
		return j
	}

	i = consumeWord(i)

	// Allow multi-word types such as "character varying" / "double precision".
	for {
		j := i
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		next := consumeWord(j)
		if next == j {
			break
		}
		i = next
	}

	// Optional "( ... )" length/precision modifier.
	if i < len(s) && s[i] == '(' {
		depth := 0
		for i < len(s) {
			if s[i] == '(' {
				depth++
			} else if s[i] == ')' {
				depth--
				if depth == 0 {
					i++
					break
				}
			}
			i++
		}
	}

	return i
}

// BuildCheckConstraintStatements computes the ADD/DROP statements that reconcile
// the table's current CHECK constraints to postgresTableSchema.Checks. It mirrors
// BuildForeignKeyStatements exactly: a desired pass that adds new constraints and
// drops-then-re-adds a same-named constraint whose predicate changed, then an
// existing pass that drops any current CHECK absent from the desired set.
//
// Drop guarding: a current CHECK is dropped ONLY when it is named and that name
// is absent from the desired set AND has not already been dropped in the desired
// pass. There is no unconditional/blanket DROP. The desired list is therefore
// AUTHORITATIVE for table-level CHECK constraints (identical to how foreignKeys
// and indexes already behave in this plugin).
func BuildCheckConstraintStatements(p *PostgresConnection, tableName string, postgresTableSchema *schemasv1alpha4.PostgresqlTableSchema) ([]string, error) {
	statements := []string{}
	droppedNames := []string{}

	currentChecks, err := p.ListTableCheckConstraints(p.databaseName, tableName)
	if err != nil {
		return nil, err
	}

	// Desired pass: add new constraints; drop+re-add a changed one (matched by name).
	for _, desired := range postgresTableSchema.Checks {
		name := types.GeneratePostgresqlCheckName(tableName, desired)

		var matched *types.CheckConstraint
		for _, current := range currentChecks {
			if current.Name == name {
				matched = current
				break
			}
		}

		if matched != nil && checkExprEquivalent(desired.Expression, matched.Expression) {
			// A same-named, equivalent constraint already exists: no-op (idempotent).
			continue
		}

		if matched != nil {
			// Same name, different predicate. PostgreSQL has no ALTER CONSTRAINT for
			// the predicate, so drop then re-add. Record the name so the existing
			// pass below does not drop it a second time.
			statements = append(statements, RemoveCheckConstraintStatement(tableName, matched))
			droppedNames = append(droppedNames, matched.Name)
		}

		statements = append(statements, AddCheckConstraintStatement(tableName, desired))
	}

	// Existing pass: drop any current CHECK not present in the desired set.
	for _, current := range currentChecks {
		isDesired := false
		for _, desired := range postgresTableSchema.Checks {
			if types.GeneratePostgresqlCheckName(tableName, desired) == current.Name {
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

		statements = append(statements, RemoveCheckConstraintStatement(tableName, current))
	}

	return statements, nil
}
