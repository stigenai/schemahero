package postgres

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// rowSecurityToggleStatement maps a tri-state toggle string (desired) to a
// migration statement, comparing against the current introspected state. It is
// shared by the ENABLE/DISABLE and FORCE/NOFORCE toggles:
//
//   - desired == ""            -> "" (leave unmanaged, emit nothing)
//   - desired == onWord  (e.g. "enable")  -> build(table, true)  if !current
//   - desired == offWord (e.g. "disable") -> build(table, false) if  current
//   - anything else            -> error (catches typos like "enabel")
//
// Returning "" when the desired state already matches the current state is what
// makes a re-plan idempotent. The comparison is case-insensitive.
func rowSecurityToggleStatement(tableName, desired string, current bool, onWord, offWord string, build func(string, bool) string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(desired)) {
	case "":
		return "", nil
	case onWord:
		if !current {
			return build(tableName, true), nil
		}
		return "", nil
	case offWord:
		if current {
			return build(tableName, false), nil
		}
		return "", nil
	default:
		return "", errors.Errorf("invalid rowLevelSecurity toggle %q (want %q, %q, or empty)", desired, onWord, offWord)
	}
}

// enableRowLevelSecurityStatement renders ALTER TABLE ... ENABLE/DISABLE ROW
// LEVEL SECURITY. The table is qualified via qualifyTableName (the schema-safe
// renderer: "app.users" -> "app"."users", bare "users" -> "users") — NOT
// pgx.Identifier{tableName}, which would mis-quote a dotted name as a single
// identifier (the fork's prior schema-qualification bug class).
//
// This toggle is non-destructive: it only changes whether RLS is enforced; it
// never drops policies or data.
func enableRowLevelSecurityStatement(tableName string, enable bool) string {
	verb := "enable"
	if !enable {
		verb = "disable"
	}
	return fmt.Sprintf("alter table %s %s row level security", qualifyTableName(tableName), verb)
}

// forceRowLevelSecurityStatement renders ALTER TABLE ... [NO] FORCE ROW LEVEL
// SECURITY (whether RLS also applies to the table owner). Non-destructive.
func forceRowLevelSecurityStatement(tableName string, force bool) string {
	verb := "force"
	if !force {
		verb = "no force"
	}
	return fmt.Sprintf("alter table %s %s row level security", qualifyTableName(tableName), verb)
}

// RemovePolicyStatement renders a guarded DROP POLICY. It is ALWAYS
// "drop policy if exists" so a re-run (or a partial prior apply, or a concurrent
// reconcile) cannot wedge the plan with a 42704 "policy does not exist" — the
// same idempotency guard RemoveIndexStatement uses for unique indexes and
// dropTriggerStatement uses for triggers.
//
// DROP POLICY removes only the policy; it never cascades to table data. It does,
// however, change row visibility for the affected command while absent, so the
// diff only ever reaches this for a policy the user removed from the spec or a
// structural recreate (see BuildRowLevelSecurityStatements) — never for a policy
// the user still declares unchanged.
//
// The policy name is sanitized via pgx.Identifier (quoting/escaping) and the
// table via qualifyTableName (schema-safe). We deliberately do NOT hand-build
// the "schema.table" string nor %q the name — the fork's prior diff bugs were
// exactly hand-rolled quoting/qualification.
func RemovePolicyStatement(tableName string, policyName string) string {
	return fmt.Sprintf("drop policy if exists %s on %s",
		pgx.Identifier{policyName}.Sanitize(),
		qualifyTableName(tableName))
}

// CreatePolicyStatement renders a CREATE POLICY. PostgreSQL has no
// CREATE POLICY ... IF NOT EXISTS, so the diff guarantees the policy is absent
// before this runs (new policy, or immediately after a guarded drop).
//
// Defaults are emitted by OMISSION to match how pg_policy stores them, so a
// re-plan is a clean no-op:
//   - PERMISSIVE  -> omit "AS RESTRICTIVE"
//   - command ALL -> omit "FOR <cmd>"
//   - empty roles -> omit "TO ..." (defaults to PUBLIC)
//
// Quoting discipline (mirrors foreignkey.go / trigger.go):
//   - table: qualifyTableName (schema-safe), never pgx.Identifier{tableName}
//   - policy name: pgx.Identifier{}.Sanitize()
//   - roles: SanitizeArray (each role identifier-quoted)
//   - using / withCheck: emitted VERBATIM inside parentheses, NEVER quoted or
//     escaped (they are raw boolean SQL, exactly like a trigger WHEN condition or
//     a CHECK expression — over-quoting them was the fork's column-default bug class)
func CreatePolicyStatement(tableName string, policy *schemasv1alpha4.PostgresqlTablePolicy) string {
	var b strings.Builder
	fmt.Fprintf(&b, "create policy %s on %s",
		pgx.Identifier{policy.Name}.Sanitize(),
		qualifyTableName(tableName))

	if strings.EqualFold(strings.TrimSpace(policy.Permissive), "RESTRICTIVE") {
		b.WriteString(" as restrictive")
	}

	cmd := strings.ToUpper(strings.TrimSpace(policy.Command))
	if cmd == "" {
		cmd = "ALL"
	}
	if cmd != "ALL" {
		fmt.Fprintf(&b, " for %s", strings.ToLower(cmd))
	}

	if len(policy.Roles) > 0 {
		fmt.Fprintf(&b, " to %s", strings.Join(SanitizeArray(policy.Roles), ", "))
	}

	if policy.Using != nil {
		fmt.Fprintf(&b, " using (%s)", *policy.Using)
	}

	if policy.WithCheck != nil {
		fmt.Fprintf(&b, " with check (%s)", *policy.WithCheck)
	}

	return b.String()
}
