package postgres

import (
	"fmt"
	"regexp"
	"strings"
)

// oidClassRegexp matches a single-quoted string literal followed by a "::type"
// cast (the form PostgreSQL stores/renders column defaults in, e.g.
// 'pending'::lifecycle_state). The capture group KEEPS the surrounding quotes so
// the stripped result is still a valid SQL string literal ('pending'), not a bare
// word (pending). This is load-bearing for the column-default diff: the desired
// default comes from the spec WITH quotes ('pending'), so dropping the quotes here
// made the introspected default ('pending') compare unequal to the spec and
// re-emit ALTER COLUMN SET DEFAULT on every plan. formatColumnDefault re-quotes a
// bare value anyway, so keeping the quotes leaves rendering unchanged.
var oidClassRegexp = regexp.MustCompile(`('.*')::.+`)

func stripOIDClass(value string) string {
	matches := oidClassRegexp.FindStringSubmatch(value)
	if len(matches) == 2 {
		return matches[1]
	}
	return value
}

// formatColumnDefault renders a column default value for use after the SQL
// `default ` keyword (or as the RHS of an `=` in a backfill UPDATE), after
// stripping any introspected `::type` cast.
//
// A value that is ALREADY a SQL literal/expression must be emitted as-is:
//   - an already single-quoted string (the Table-spec convention, e.g. 'pending')
//     would otherwise gain a second surrounding quote layer and become invalid
//     SQL (a doubled quote pair around the word, SQLSTATE 42601);
//   - a function call such as gen_random_uuid() would otherwise be quoted into a
//     string -> 'gen_random_uuid()', which is not a valid value (SQLSTATE 22P02).
//
// Everything else (a bare plain string, including numeric-looking strings) is
// wrapped in single quotes — Postgres coerces the quoted literal to the column
// type, which is safe for both numeric and text columns.
//
// Both the CREATE/ADD COLUMN path (column.go) and the ALTER COLUMN SET DEFAULT
// path (alter.go) MUST use this so an identical default renders identically;
// the alter path previously hardcoded a quoted "%s", causing the bugs above.
func formatColumnDefault(raw string) string {
	value := stripOIDClass(raw)
	if strings.HasPrefix(value, "'") || strings.Contains(value, "(") {
		return value
	}
	return fmt.Sprintf("'%s'", value)
}
