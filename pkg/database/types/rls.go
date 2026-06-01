package types

import (
	"sort"
	"strings"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// RowLevelSecurity is the dialect-neutral mirror of a table's pg_class
// relrowsecurity / relforcerowsecurity flags.
type RowLevelSecurity struct {
	Enabled bool
	Forced  bool
}

// Policy is the introspection-side mirror of a PostgreSQL row-level security
// policy (pg_policy). It is the dialect-neutral counterpart to the apis
// PostgresqlTablePolicy, normalized so that an introspected policy and the
// converted desired policy compare equal when they are semantically identical.
type Policy struct {
	Name string
	// Command is the normalized statement class: ALL | SELECT | INSERT | UPDATE | DELETE.
	Command string
	// Permissive is true for a PERMISSIVE policy, false for RESTRICTIVE.
	Permissive bool
	// Roles is the set of role names the policy applies to. A PUBLIC policy is
	// normalized to ["public"] (pg_policy stores PUBLIC as the pseudo-role OID 0,
	// which has no pg_authid row).
	Roles []string
	// Using is the USING expression (raw SQL), or nil when the policy has none.
	Using *string
	// WithCheck is the WITH CHECK expression (raw SQL), or nil when none.
	WithCheck *string
}

// normalizePolicyCommand maps an empty command to the PostgreSQL default "ALL"
// and upper-cases it so "all"/"All"/"" all compare equal to the catalog's ALL.
func normalizePolicyCommand(command string) string {
	c := strings.ToUpper(strings.TrimSpace(command))
	if c == "" {
		return "ALL"
	}
	return c
}

// normalizePolicyRoles returns a sorted copy of roles, defaulting an empty list
// to ["public"] so a spec that omits roles matches an introspected PUBLIC policy.
func normalizePolicyRoles(roles []string) []string {
	if len(roles) == 0 {
		return []string{"public"}
	}
	out := make([]string, len(roles))
	copy(out, roles)
	sort.Strings(out)
	return out
}

// normalizePolicyExpr lower-cases and collapses internal whitespace of a raw-SQL
// policy expression for COMPARISON ONLY (never used to build emitted SQL). It is
// best-effort: pg_get_expr re-renders an expression canonically (adds parens,
// ::type casts, schema-qualifies functions), so a user-supplied expression that
// is not already in that canonical form may still differ here. The diff layer
// therefore does NOT recreate a policy solely on an expression difference (see
// BuildRowLevelSecurityStatements); this normalization only lets an
// already-canonical expression (e.g. one copied from a prior plan or written in
// canonical form) short-circuit to a clean no-op.
func normalizePolicyExpr(expr *string) string {
	if expr == nil {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(*expr)), " ")
}

// Equals reports whether two policies are semantically identical. Name, Command,
// Permissive, the Roles set, and the (normalized) Using/WithCheck expressions
// must all match. It is used to short-circuit a same-named policy to a no-op so
// a re-plan of an unchanged schema emits nothing.
func (p *Policy) Equals(other *Policy) bool {
	if p == nil || other == nil {
		return p == other
	}

	if p.Name != other.Name {
		return false
	}

	if normalizePolicyCommand(p.Command) != normalizePolicyCommand(other.Command) {
		return false
	}

	if p.Permissive != other.Permissive {
		return false
	}

	if !rolesEqual(p.Roles, other.Roles) {
		return false
	}

	if normalizePolicyExpr(p.Using) != normalizePolicyExpr(other.Using) {
		return false
	}

	if normalizePolicyExpr(p.WithCheck) != normalizePolicyExpr(other.WithCheck) {
		return false
	}

	return true
}

// AttributesEqual reports whether the STRUCTURAL attributes of two policies match:
// Command, Permissive, and the Roles set. It deliberately EXCLUDES the
// Using/WithCheck expressions.
//
// This is the comparator the diff uses to decide a destructive drop+recreate of a
// same-named policy. PostgreSQL's pg_get_expr re-renders policy expressions
// canonically, so comparing the user's raw USING/WITH CHECK text against the
// introspected text would mismatch on cosmetic differences and force an endless
// drop+recreate churn (and a brief window where the policy is absent — a
// row-visibility security gap). By keying the recreate decision on the
// structural attributes only, an expression-only edit is NOT auto-applied; it
// requires the policy to be renamed or manually dropped. This matches the
// blueprint's documented v1 safety boundary.
func (p *Policy) AttributesEqual(other *Policy) bool {
	if p == nil || other == nil {
		return p == other
	}

	if normalizePolicyCommand(p.Command) != normalizePolicyCommand(other.Command) {
		return false
	}

	if p.Permissive != other.Permissive {
		return false
	}

	return rolesEqual(p.Roles, other.Roles)
}

// rolesEqual compares two role lists as sets, normalizing empty -> ["public"].
func rolesEqual(a, b []string) bool {
	an := normalizePolicyRoles(a)
	bn := normalizePolicyRoles(b)
	if len(an) != len(bn) {
		return false
	}
	for i := range an {
		if an[i] != bn[i] {
			return false
		}
	}
	return true
}

// PostgresqlSchemaPolicyToPolicy converts a desired apis policy to the
// introspection-side Policy, normalizing defaults (empty command -> ALL, empty
// permissive -> PERMISSIVE, empty roles -> ["public"]) so it compares equal to
// what pg_policy returns for the same intent.
func PostgresqlSchemaPolicyToPolicy(schemaPolicy *schemasv1alpha4.PostgresqlTablePolicy) *Policy {
	permissive := true
	if strings.EqualFold(strings.TrimSpace(schemaPolicy.Permissive), "RESTRICTIVE") {
		permissive = false
	}

	return &Policy{
		Name:       schemaPolicy.Name,
		Command:    normalizePolicyCommand(schemaPolicy.Command),
		Permissive: permissive,
		Roles:      normalizePolicyRoles(schemaPolicy.Roles),
		Using:      schemaPolicy.Using,
		WithCheck:  schemaPolicy.WithCheck,
	}
}

// PolicyCommandFromCatalogChar maps a pg_policy.polcmd char to a Command string.
// '*' = ALL, 'r' = SELECT, 'a' = INSERT, 'w' = UPDATE, 'd' = DELETE.
func PolicyCommandFromCatalogChar(polcmd string) string {
	switch polcmd {
	case "*":
		return "ALL"
	case "r":
		return "SELECT"
	case "a":
		return "INSERT"
	case "w":
		return "UPDATE"
	case "d":
		return "DELETE"
	default:
		return "ALL"
	}
}
