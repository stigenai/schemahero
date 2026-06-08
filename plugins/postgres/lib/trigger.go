package postgres

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

func triggerCreateStatement(trigger *schemasv1alpha4.PostgresqlTableTrigger, tableName string) (string, error) {
	triggerEventSyntax, err := triggerEvent(trigger)
	if err != nil {
		return "", errors.Wrap(err, "failed to create trigger event syntax")
	}

	o := "trigger"
	if trigger.ConstraintTrigger != nil && *trigger.ConstraintTrigger {
		o = "constraint trigger"
	}

	// Render the table schema-safely (sanitizeTableName: "global.cells" ->
	// "global"."cells", bare "cells" -> "cells"). Using %q here would quote a
	// schema-qualified name as a SINGLE identifier ("global.cells") and emit the
	// trigger on a nonexistent relation. This is the same renderer (and the same
	// hard-won lesson) as dropTriggerStatement.
	stmt := fmt.Sprintf(`create %s %q %s on %s`, o, trigger.Name, triggerEventSyntax, sanitizeTableName(tableName))

	forEachStatement := true // pg default
	if trigger.ForEachRow != nil && *trigger.ForEachRow {
		forEachStatement = false
	}

	if forEachStatement {
		stmt = fmt.Sprintf("%s for each statement", stmt)
	} else {
		stmt = fmt.Sprintf("%s for each row", stmt)
	}

	if trigger.Condition != nil {
		stmt = fmt.Sprintf("%s when (%s)", stmt, *trigger.Condition)
	}

	if trigger.Execute == nil {
		stmt = fmt.Sprintf("%s execute procedure %s", stmt, trigger.ExecuteProcedure)
	} else {
		switch trigger.Execute.Type {
		case "Procedure":
			if trigger.Execute.Name != "" {
				stmt = fmt.Sprintf("%s execute procedure %s", stmt, getQualifiedExecuteName(trigger.Execute.Name, trigger.Execute.Schema, trigger.Execute.Params))
			} else if trigger.ExecuteProcedure != "" {
				stmt = fmt.Sprintf("%s execute procedure %s", stmt, trigger.ExecuteProcedure)
			} else {
				return "", errors.New("when using procedure execute type you have to define a procedure under execute")
			}
		case "Function":
			if trigger.Execute.Name == "" {
				return "", errors.New("when using function execute type you have to define a function under execute")
			}
			stmt = fmt.Sprintf("%s execute function %s", stmt, getQualifiedExecuteName(trigger.Execute.Name, trigger.Execute.Schema, trigger.Execute.Params))
		default:
			stmt = fmt.Sprintf("%s execute procedure %s", stmt, trigger.ExecuteProcedure)
		}
	}

	return stmt, nil
}

// dropTriggerStatement renders a guarded DROP for a trigger. It is ALWAYS
// "drop trigger if exists" so a re-run (or a partial prior apply that already
// removed the trigger, or a concurrent reconcile) cannot wedge the plan with a
// 42704 "trigger does not exist". DROP TRIGGER removes only the trigger; it
// never cascades to table data or to the referenced function, so this is the
// low-risk class of drop — but it is still guarded with IF EXISTS.
//
// The trigger name is sanitized via pgx.Identifier (handles quoting/escaping)
// and the table via sanitizeTableName (the schema-safe renderer: "app.users"
// -> "app"."users", bare "users" -> "users"). We deliberately do NOT %q the
// name or hand-build the schema.table string — the fork's prior diff bugs were
// exactly hand-rolled quoting/qualification.
func dropTriggerStatement(triggerName string, tableName string) string {
	return fmt.Sprintf(`drop trigger if exists %s on %s`,
		pgx.Identifier{triggerName}.Sanitize(),
		sanitizeTableName(tableName))
}

// normalizeTriggerDefinition reduces a trigger definition to a canonical form
// for COMPARISON ONLY (it is never used to build emitted SQL). It:
//   - lowercases,
//   - removes double-quote identifier delimiters,
//   - collapses internal whitespace runs to a single space and trims,
//   - strips a trailing ';',
//   - canonicalizes "execute procedure" -> "execute function",
//   - strips the redundant "public." schema qualifier from every identifier.
//
// This absorbs the cosmetic differences between what triggerCreateStatement
// emits and what pg_get_triggerdef(oid, true) reports:
//   - Quoting: triggerCreateStatement quotes the trigger name and table
//     ("tt", "users"), whereas pg_get_triggerdef leaves lowercase identifiers
//     unquoted (tt, users). Stripping the double-quote delimiter makes a
//     lowercase identifier compare equal either way. (Double quotes only ever
//     delimit identifiers here; SQL string literals in a WHEN condition use
//     single quotes, which are preserved.)
//   - Keyword casing / spacing.
//   - EXECUTE PROCEDURE vs EXECUTE FUNCTION: PG12+'s pg_get_triggerdef always
//     emits "EXECUTE FUNCTION", whereas triggerCreateStatement may emit
//     "EXECUTE PROCEDURE" (legacy ExecuteProcedure or Execute.Type=Procedure);
//     they are exact synonyms, so the token is collapsed.
//   - Schema qualification: pg_get_triggerdef FULLY schema-qualifies both the
//     table and the executed function ("ON public.users ... EXECUTE FUNCTION
//     public.fn()"), whereas triggerCreateStatement omits the schema for the
//     default "public" schema ("ON users ... EXECUTE FUNCTION fn()"). Without
//     reconciling this, a perfectly idempotent trigger on a public-schema
//     function would churn (a guarded but pointless drop+recreate) on EVERY plan.
//     stripRedundantPublicSchema drops the implicit "public." qualifier from both
//     sides so the natural bare form and the catalog's qualified form compare
//     equal. A function in a non-default schema is still rendered with that schema
//     on both sides (the spec must set execute.schema), so it is unaffected; a
//     literal containing "public." is not — only a space-delimited schema
//     qualifier preceding an identifier is stripped.
func normalizeTriggerDefinition(def string) string {
	s := strings.ToLower(def)
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ";")
	s = strings.ReplaceAll(s, "execute procedure ", "execute function ")
	s = stripRedundantPublicSchema(s)
	return s
}

// stripRedundantPublicSchema removes the implicit "public." schema qualifier that
// pg_get_triggerdef prepends to identifiers (the table and the executed function),
// so a definition that qualifies them and one that leaves them bare compare equal.
//
// It operates on an already-lowercased, whitespace-collapsed, unquoted string and
// only removes "public." when it appears as a schema qualifier: immediately after
// a word boundary (start, space, or '(') and immediately followed by an identifier
// start character. That guards a string literal or a column literally named
// "public" from being mangled — only the "public.<ident>" qualifier shape is
// rewritten to "<ident>".
func stripRedundantPublicSchema(s string) string {
	const qualifier = "public."

	var b strings.Builder
	for i := 0; i < len(s); {
		if hasQualifierAt(s, i, qualifier) {
			i += len(qualifier)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// hasQualifierAt reports whether the schema qualifier q (e.g. "public.") occurs at
// index i as a real qualifier: preceded by a word boundary (start of string, a
// space, or an opening paren) and followed by an identifier-start character. This
// avoids stripping "public." when it is part of a larger token (e.g. "mypublic.x")
// or not actually qualifying an identifier.
func hasQualifierAt(s string, i int, q string) bool {
	if i+len(q) > len(s) || s[i:i+len(q)] != q {
		return false
	}
	if i > 0 {
		prev := s[i-1]
		if prev != ' ' && prev != '(' {
			return false
		}
	}
	next := i + len(q)
	if next >= len(s) {
		return false
	}
	c := s[next]
	isIdentStart := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
	return isIdentStart
}

// BuildTriggerStatements diffs the triggers declared in postgresTableSchema
// against the triggers currently on the (already-existing) table and returns the
// migration statements. It mirrors BuildForeignKeyStatements' two-pass
// create/drop structure: a desired pass that creates new triggers and
// drop+recreates changed ones (matched by name), then an existing pass that
// drops any current trigger absent from the desired set.
//
// It must be called LAST in PlanPostgresTable's existing-table branch (after
// columns/PK/FK/check/index) because a trigger can reference a newly-added
// column or function, and it must NOT be called in the create-table branch
// (CreateTableStatements already emits the triggers there).
//
// PostgreSQL has no ALTER TRIGGER that can change a trigger's definition, so the
// strategy is drop+recreate on any definition change, idempotent (emits nothing)
// when unchanged — matching the FK/index behavior. Every drop is guarded with
// IF EXISTS (see dropTriggerStatement) and DROP TRIGGER never touches table data
// or the referenced function, so this is the low-risk drop class.
func BuildTriggerStatements(p *PostgresConnection, tableName string, postgresTableSchema *schemasv1alpha4.PostgresqlTableSchema) ([]string, error) {
	statements := []string{}
	droppedNames := []string{}

	// Desired-trigger source: the same precedence CreateTableStatements uses —
	// the deprecated JSONTriggers wins only when populated.
	triggers := postgresTableSchema.Triggers
	if len(postgresTableSchema.JSONTriggers) > 0 {
		triggers = postgresTableSchema.JSONTriggers
	}

	currentTriggers, err := p.ListTableTriggers(tableName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list table triggers")
	}

	// Qualify the table exactly as CreateTableStatements does so a recreated
	// trigger's CREATE matches the create-time rendering.
	qualifiedTableName := tableName
	if postgresTableSchema.Schema != "" && postgresTableSchema.Schema != "public" {
		qualifiedTableName = fmt.Sprintf("%s.%s", postgresTableSchema.Schema, tableName)
	}

	// Desired pass: create new triggers; drop+recreate a changed one (matched by name).
	for _, trigger := range triggers {
		// A trigger with no name cannot be matched against pg_get_triggerdef nor
		// dropped, so it cannot be managed by the diff path. Fail loudly rather
		// than emit an untrackable CREATE. (The create-table path tolerates an
		// unnamed trigger; the alter path must not.)
		if trigger.Name == "" {
			return nil, errors.New("trigger requires a name to be managed on an existing table")
		}

		want, err := triggerCreateStatement(trigger, qualifiedTableName)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create trigger statement")
		}

		var matched *pgTrigger
		for i := range currentTriggers {
			if currentTriggers[i].Name == trigger.Name {
				matched = &currentTriggers[i]
				break
			}
		}

		if matched == nil {
			// New trigger: emit only the CREATE.
			statements = append(statements, want)
			continue
		}

		if normalizeTriggerDefinition(want) == normalizeTriggerDefinition(matched.Definition) {
			// Same-named, equivalent trigger already exists: no-op (idempotent).
			continue
		}

		// Same name, different definition. No ALTER TRIGGER for the definition, so
		// emit a guarded drop immediately followed by the create so the pair applies
		// atomically per-trigger. Record the name so the existing pass does not drop
		// it a second time.
		statements = append(statements, dropTriggerStatement(matched.Name, tableName))
		statements = append(statements, want)
		droppedNames = append(droppedNames, matched.Name)
	}

	// Existing pass: drop any current trigger not present in the desired set.
	for _, current := range currentTriggers {
		isDesired := false
		for _, trigger := range triggers {
			if trigger.Name == current.Name {
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

		statements = append(statements, dropTriggerStatement(current.Name, tableName))
	}

	return statements, nil
}

func triggerEvent(trigger *schemasv1alpha4.PostgresqlTableTrigger) (string, error) {
	if len(trigger.Events) == 0 {
		return "", errors.New("trigger missing events")
	}

	// build the event which could be like:
	//   after insert or update of col1, col2

	// all triggers must be the same temporal event (after, before, instead of)
	temporal := ""
	if strings.HasPrefix(strings.ToLower(trigger.Events[0]), "after") {
		temporal = "after"
	} else if strings.HasPrefix(strings.ToLower(trigger.Events[0]), "before") {
		temporal = "before"
	} else if strings.HasPrefix(strings.ToLower(trigger.Events[0]), "instead of") {
		temporal = "instead of"
	} else {
		return "", errors.New("unable to parse trigger")
	}

	events := []string{}
	for _, event := range trigger.Events {
		event := strings.TrimSpace(strings.ToLower(event))

		if strings.HasPrefix(event, "after") {
			events = append(events, strings.TrimPrefix(event, "after"))
			continue
		} else if strings.HasPrefix(event, "before") {
			events = append(events, strings.TrimPrefix(event, "before"))
			continue
		} else if strings.HasPrefix(event, "instead of") {
			events = append(events, strings.TrimPrefix(event, "instead of"))
			continue
		}
	}

	return fmt.Sprintf("%s%s", temporal, strings.Join(events, " or")), nil
}
