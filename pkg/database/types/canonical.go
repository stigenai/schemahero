package types

import "strings"

// CanonicalizeSQLExpr reduces a raw SQL boolean/expression fragment to a
// canonical comparison form: lowercased, with all "::type" casts removed, all
// parentheses dropped, and internal whitespace collapsed to single spaces.
//
// It exists because PostgreSQL ALWAYS canonicalizes the expressions it stores and
// renders back via pg_get_expr / pg_get_constraintdef: a natural fragment such as
// "lower(email)" round-trips as "lower((email)::text)" and a comparison against an
// empty string literal round-trips with "(phone)::text" / "::text" casts added.
// A comparator that only lowercases and collapses
// whitespace (the previous index/EXCLUDE behavior) therefore sees a difference on
// EVERY plan for any expression/partial index or EXCLUDE written in natural form,
// forcing a needless drop+recreate (a heavy lock + index rebuild, and for a unique
// index a momentary uniqueness-guard gap). Canonicalizing BOTH the desired (spec)
// and the introspected text the same way makes the natural and canonical forms
// compare equal, so a steady-state plan is a true no-op.
//
// This is the single shared reference implementation; the postgres plugin's CHECK
// comparator (normalizeCheckExpr) and the index/EXCLUDE comparators all route
// through it so the three expression-diff paths cannot drift. When canonicalization
// is ambiguous it errs toward treating two fragments as EQUAL (a missed no-op alter
// is far cheaper than a spurious destructive churn against production).
func CanonicalizeSQLExpr(s string) string {
	s = strings.ToLower(s)
	s = stripTypeCasts(s)

	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		switch r {
		case '(', ')':
			// Drop parens entirely: PostgreSQL adds/removes redundant grouping
			// parens freely, so they carry no comparison signal here.
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
	// Normalize whitespace around commas: PostgreSQL renders list/array separators
	// as ", " (comma + space), while a hand-authored spec usually writes ",". The
	// surrounding whitespace carries no SQL meaning, so dropping it on both sides
	// makes the two forms compare equal — otherwise a CHECK/index/EXCLUDE using a
	// list literal such as "x = ANY (ARRAY['a','b'])" churns drop+recreate on every
	// plan against pg's stored "ARRAY['a'::text, 'b'::text]". Whitespace has already
	// been collapsed to single spaces above, so two replacements suffice.
	out := strings.TrimSpace(b.String())
	out = strings.ReplaceAll(out, ", ", ",")
	out = strings.ReplaceAll(out, " ,", ",")
	return out
}

// stripTypeCasts removes PostgreSQL "::type" cast suffixes, including ones with a
// parenthesized length/modifier such as "::character varying" or "::numeric(10,2)".
// It scans for "::" and drops the following type token (an identifier run,
// optional whitespace-separated words like "character varying", and an optional
// "( ... )" modifier).
func stripTypeCasts(s string) string {
	// out accumulates the result as a byte slice (rather than a strings.Builder) so
	// the "::type" branch can look back at, and rewrite, the bytes it already
	// emitted — specifically to UNQUOTE a numeric literal that PostgreSQL quoted
	// before casting (it renders an unquoted "-273" as "'-273'::integer"). A user
	// writes such a literal unquoted in their spec, so without this the natural
	// "x >= -273" would never equal the canonical "x >= '-273'::integer" and a
	// CHECK/index/EXCLUDE using a negative (or otherwise quote-cast) numeric literal
	// would churn on every plan. String literals are unaffected: PostgreSQL quotes
	// them AND the user quotes them, so both sides keep the quotes and already match.
	out := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == ':' && s[i+1] == ':' {
			i += 2
			end := skipTypeToken(s, i)
			// Only unquote the preceding literal when the cast target is a NUMERIC
			// type. PostgreSQL casts a numeric constant as "'-273'::integer" but a
			// string constant as "'5'::text" / "'a'::character varying"; unquoting a
			// string literal (e.g. turning "'5'::text" into 5) would make a genuine
			// string value compare equal to a number, so it is restricted to numeric
			// cast targets, where the quoting is purely a rendering artifact.
			if isNumericCastType(s[i:end]) {
				out = unquoteTrailingNumericLiteral(out)
			}
			i = end
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

// isNumericCastType reports whether the cast type token t (already lowercased by
// the caller's canonicalization) names a PostgreSQL numeric type, for which a
// quoted literal constant is just a rendering artifact and may be safely unquoted.
// Any length/precision modifier (e.g. "numeric(10,2)") is ignored by comparing
// only the leading type words.
func isNumericCastType(t string) bool {
	name := strings.TrimSpace(t)
	// Drop an optional "( ... )" modifier so "numeric(10,2)" matches "numeric".
	if p := strings.IndexByte(name, '('); p >= 0 {
		name = strings.TrimSpace(name[:p])
	}
	switch name {
	case "smallint", "integer", "int", "int2", "int4", "int8", "bigint",
		"numeric", "decimal", "real", "double precision", "float", "float4", "float8":
		return true
	default:
		return false
	}
}

// unquoteTrailingNumericLiteral removes the surrounding single quotes from a
// numeric literal at the END of out, e.g. turns "...>= '-273'" into "...>= -273".
// It only acts when the trailing token is a single-quoted run whose content is a
// valid numeric literal (optional leading sign, digits, optional single decimal
// point); any other quoted content (a string literal) is left untouched so two
// genuinely different values do not collapse. This is called only immediately
// before a "::type" cast is dropped, matching PostgreSQL's quote-then-cast
// rendering of numeric constants.
func unquoteTrailingNumericLiteral(out []byte) []byte {
	n := len(out)
	if n < 2 || out[n-1] != '\'' {
		return out
	}
	// Find the matching opening quote for the trailing closing quote.
	start := -1
	for j := n - 2; j >= 0; j-- {
		if out[j] == '\'' {
			start = j
			break
		}
	}
	if start < 0 {
		return out
	}
	content := out[start+1 : n-1]
	if !isNumericLiteral(content) {
		return out
	}
	// Replace "'<numeric>'" with "<numeric>".
	return append(out[:start], content...)
}

// isNumericLiteral reports whether b is a plain numeric literal: an optional
// leading '+'/'-', one or more digits, and at most one '.'. It deliberately
// rejects anything else (letters, exponents, multiple dots) so only unambiguous
// numbers are unquoted.
func isNumericLiteral(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	i := 0
	if b[i] == '+' || b[i] == '-' {
		i++
	}
	digits := 0
	dots := 0
	for ; i < len(b); i++ {
		switch {
		case b[i] >= '0' && b[i] <= '9':
			digits++
		case b[i] == '.':
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0
}

// typeContinuationWords is the CLOSED set of second/third words that can extend a
// multi-word PostgreSQL type name. skipTypeToken consumes a following word ONLY
// when it is in this set; this is load-bearing. The naive "consume any following
// identifier" approach swallows SQL operator keywords and column references that
// follow an unparenthesized cast — e.g. pg_get_constraintdef(oid, true) renders
// "x >= '-273'::integer AND y" with NO parens around the cast, and treating "and"
// (then "y") as type words would eat the rest of the predicate, corrupting the
// canonical form and forcing a needless CHECK drop+recreate on every plan. The set
// covers the real multi-word/qualified types: "character varying", "bit varying",
// "double precision", "time/timestamp with[out] time zone".
var typeContinuationWords = map[string]bool{
	"varying":   true, // character varying, bit varying
	"precision": true, // double precision
	"with":      true, // time(stamp) with time zone
	"without":   true, // time(stamp) without time zone
	"time":      true, // ... with/without TIME zone
	"zone":      true, // ... time ZONE
}

// skipTypeToken advances past a type name beginning at index i: a run of
// identifier characters, optionally followed by additional whitespace-separated
// type-continuation words (e.g. "character varying", "double precision",
// "timestamp without time zone") and an optional balanced "( ... )" modifier. It
// returns the index just past the type. Only words in typeContinuationWords extend
// the type, so an operator keyword or column reference after an unparenthesized
// cast terminates the type rather than being swallowed.
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

	// Extend across a schema-qualified type name: "::global.cell_type".
	// PostgreSQL renders enum casts in schema-qualified form when the enum is not
	// in the search_path, e.g. "::global.cell_type" rather than "::cell_type". The
	// dot is not an identifier character so consumeWord stops after "global"; we
	// must consume the "." and the following word to strip the full cast token.
	// This cannot swallow a table-qualified reference that follows an unparenthesized
	// cast because a "schema.table" reference is always preceded by whitespace or an
	// operator, not immediately after the type name.
	if i < len(s) && s[i] == '.' {
		j := i + 1
		next := consumeWord(j)
		if next > j {
			i = next
		}
	}

	// Extend across multi-word type names, but ONLY for known continuation words.
	for {
		j := i
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		next := consumeWord(j)
		if next == j {
			break
		}
		if !typeContinuationWords[strings.ToLower(s[j:next])] {
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

	// Optional trailing array marker(s). PostgreSQL renders an array type as
	// "type[]" (e.g. "::text[]", "::integer[]") and a multi-dimensional array as
	// "type[][]". Without consuming it, the "::type" branch strips "text" but leaves
	// the "[]" — and the main loop keeps it (brackets are not parens it drops) — so a
	// varchar-column CHECK such as "(outcome)::text = ANY ((ARRAY[...])::text[])"
	// canonicalizes with a trailing "[]" and never equals the natural "outcome =
	// ANY (ARRAY[...])", forcing a drop+recreate on every plan. The "[" must
	// immediately follow the type token (pg emits no space), so this cannot swallow a
	// parenthesized cast's array subscript "(x::type)[i]" (that "[" follows a ")").
	for i < len(s) && s[i] == '[' {
		i++
		for i < len(s) && s[i] != ']' {
			i++
		}
		if i < len(s) && s[i] == ']' {
			i++
		}
	}

	return i
}
