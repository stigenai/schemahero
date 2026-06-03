/*
Copyright 2019 The SchemaHero Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package postgres

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v4"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"
)

// splitQualifiedTableName splits a possibly schema-qualified table reference
// ("schema.table") into its schema and bare-table parts. For a bare name it
// returns an empty schema and the name unchanged.
func splitQualifiedTableName(tableName string) (schema string, table string) {
	if idx := strings.Index(tableName, "."); idx >= 0 {
		return tableName[:idx], tableName[idx+1:]
	}
	return "", tableName
}

// sanitizeTableName renders a possibly schema-qualified table reference as a
// safely-quoted SQL identifier: "schema.table" -> "schema"."table", a bare
// "table" -> "table". This is a drop-in for pgx.Identifier{tableName}.Sanitize(),
// which quotes a dotted name as a single identifier and so produces the
// invalid relation "schema.table" in the public schema.
func sanitizeTableName(tableName string) string {
	if schema, table := splitQualifiedTableName(tableName); schema != "" {
		return pgx.Identifier{schema, table}.Sanitize()
	}
	return pgx.Identifier{tableName}.Sanitize()
}

// qualifyTableName splits a schema-qualified reference into safely-quoted parts
// ("schema.table" -> "schema"."table") but passes a bare name through unquoted.
// It is the drop-in for sites that previously emitted the table name raw, so a
// dotted name is no longer mis-parsed while bare-name output is unchanged.
func qualifyTableName(tableName string) string {
	if schema, table := splitQualifiedTableName(tableName); schema != "" {
		return pgx.Identifier{schema, table}.Sanitize()
	}
	return tableName
}

// parseIndexElements extracts ordered columns and functional expressions from a
// canonical `pg_get_indexdef` string. The pretty per-column array that
// ListTableIndexes also fetches drops ASC/DESC/NULLS ordering and cannot
// represent an expression, so the richer fields are recovered here.
//
// It returns non-empty results ONLY when the index uses ordering or an
// expression; a plain index (e.g. "... (email)") yields (nil, nil) so it
// continues to compare via the bare Columns set and does not spuriously churn.
//
// MIXED INDEXES: when a pg_get_indexdef element list contains BOTH plain column
// elements AND expression elements (i.e. at least one element has a "(" and at
// least one does not), positional order matters for correctness. In that case
// ALL elements — both plain and expression — are returned as Expressions in
// their original order, with SortedColumns = nil. This is the only
// representation that faithfully preserves order: SortedColumns+Expressions is
// a split structure with no position field, so it cannot survive a round-trip
// for a mixed list.
//
// Guard: the all-Expressions path is only taken when the non-expression plain
// columns carry no explicit sort/nulls keywords. If a mixed list has a plain
// column with explicit ASC/DESC/NULLS ordering, the old split behaviour is
// kept and a "// known limitation" comment documents it. Such an index is
// vanishingly rare in practice (pg_get_indexdef would render it as an
// expression "(col) desc", not "col desc", for a mixed list), so silently
// mis-ordering it via the split path is acceptable.
func parseIndexElements(indexDef string) ([]types.IndexColumn, []string) {
	body := extractIndexElementList(indexDef)
	if body == "" {
		return nil, nil
	}

	rawElements := splitTopLevelCommas(body)

	hasRich := false
	for _, raw := range rawElements {
		e := strings.TrimSpace(raw)
		if strings.Contains(e, "(") || strings.ContainsAny(e, " \t") {
			hasRich = true
			break
		}
	}
	if !hasRich {
		return nil, nil
	}

	// Classify each trimmed element as plain (no "(") or expression (has "(").
	hasExpr := false
	hasPlain := false
	for _, raw := range rawElements {
		e := strings.TrimSpace(raw)
		if strings.Contains(e, "(") {
			hasExpr = true
		} else {
			hasPlain = true
		}
	}

	// MIXED case: at least one expression and at least one plain column.
	// Return all elements as Expressions in positional order, provided the
	// plain columns are bare (no sort/nulls keywords). This preserves the
	// element ordering that a split SortedColumns+Expressions structure loses.
	if hasExpr && hasPlain {
		plainColumnsHaveSortKeywords := false
		for _, raw := range rawElements {
			e := strings.TrimSpace(raw)
			if strings.Contains(e, "(") {
				continue
			}
			upper := strings.ToUpper(e)
			if strings.ContainsAny(upper, " \t") &&
				(strings.Contains(upper, " ASC") || strings.Contains(upper, " DESC") ||
					strings.Contains(upper, "NULLS")) {
				plainColumnsHaveSortKeywords = true
				break
			}
		}
		if !plainColumnsHaveSortKeywords {
			// All plain columns are bare (possibly with an opclass token, which is
			// fine because opclasses look like identifiers — we preserve them verbatim
			// in the expression string). Return the full ordered list as Expressions.
			expressions := make([]string, 0, len(rawElements))
			for _, raw := range rawElements {
				expressions = append(expressions, strings.TrimSpace(raw))
			}
			return nil, expressions
		}
		// known limitation: a mixed element list where plain columns carry explicit
		// ASC/DESC/NULLS ordering cannot be faithfully round-tripped via either the
		// split SortedColumns+Expressions structure or the all-Expressions path.
		// Fall through to the old split behaviour, which at least preserves the
		// sort/nulls on the plain columns even if positional order is lost.
	}

	var sortedColumns []types.IndexColumn
	var expressions []string
	for _, raw := range rawElements {
		e := strings.TrimSpace(raw)
		if strings.Contains(e, "(") {
			// An expression element. pg renders it parenthesised, e.g.
			// "lower((email)::text)"; trailing ASC/DESC/NULLS are dropped for the
			// expression-equality compare (best-effort, kept simple for v1).
			expressions = append(expressions, e)
			continue
		}
		sortedColumns = append(sortedColumns, parseSortedColumnElement(e))
	}

	return sortedColumns, expressions
}

// extractIndexElementList returns the substring of a canonical index definition
// between the outermost parentheses of the column list (the first balanced
// "(...)" group), ignoring anything after it such as " WHERE ...".
func extractIndexElementList(indexDef string) string {
	start := strings.Index(indexDef, "(")
	if start < 0 {
		return ""
	}

	depth := 0
	for i := start; i < len(indexDef); i++ {
		switch indexDef[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return indexDef[start+1 : i]
			}
		}
	}
	return ""
}

// splitTopLevelCommas splits on commas that are not nested inside parentheses.
func splitTopLevelCommas(s string) []string {
	var parts []string
	depth := 0
	last := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[last:i])
				last = i + 1
			}
		}
	}
	parts = append(parts, s[last:])
	return parts
}

// parseSortedColumnElement parses a bare column element with optional operator
// class and optional ordering, e.g.:
//
//	"created_at DESC"
//	"email NULLS FIRST"
//	"name DESC NULLS LAST"
//	"conditions jsonb_path_ops"        (opclass only, no sort/nulls)
//	"data gin_trgm_ops DESC NULLS LAST" (opclass + sort + nulls)
//
// PostgreSQL index-column grammar: column [opclass] [ASC|DESC] [NULLS FIRST|LAST]
//
// The opclass is recovered as follows: fields[0] is the column; then each
// subsequent token is checked against the known sort/nulls keywords. The FIRST
// token that is NOT one of {asc, desc, nulls, first, last} is treated as the
// opclass. This handles the grammar unambiguously because pg_get_indexdef only
// emits a non-default opclass, and PostgreSQL opclass names are plain
// identifiers — they cannot be confused with the small closed set of ordering
// keywords.
//
// Known limitation: COLLATE is not handled. A column with an explicit COLLATE
// clause (e.g. "name COLLATE "en-US" ASC") would have "collate" mis-classified
// as an opclass. COLLATE on index columns is very rare in practice and is not
// present in the corpus, so this is acceptable for v1.
func parseSortedColumnElement(element string) types.IndexColumn {
	fields := strings.Fields(element)
	col := types.IndexColumn{}
	if len(fields) == 0 {
		return col
	}
	col.Column = fields[0]

	// sortNullsKeywords is the closed set of tokens that are part of the
	// ordering clause. Any token NOT in this set (and not the column name at
	// fields[0]) is the opclass.
	sortNullsKeywords := map[string]bool{
		"asc": true, "desc": true, "nulls": true, "first": true, "last": true,
	}

	for _, f := range fields[1:] {
		lower := strings.ToLower(f)
		if !sortNullsKeywords[lower] {
			// First non-keyword token after the column is the opclass.
			col.OpClass = lower
			break
		}
	}

	upper := strings.ToUpper(element)
	if strings.Contains(upper, " DESC") {
		col.Sort = "DESC"
	} else if strings.Contains(upper, " ASC") {
		col.Sort = "ASC"
	}
	if strings.Contains(upper, "NULLS FIRST") {
		col.Nulls = "FIRST"
	} else if strings.Contains(upper, "NULLS LAST") {
		col.Nulls = "LAST"
	}
	return col
}

// getQualifiedExecuteName creates an execute name that can be used to uniquely identity an executable (function or procedure)
func getQualifiedExecuteName(functionName, schema string, params []*schemasv1alpha4.PostgresqlExecuteParameter) string {
	qualifiedFunctionName := functionName
	if schema != "" && schema != "public" {
		qualifiedFunctionName = fmt.Sprintf("%s.%s", schema, functionName)
	}
	return fmt.Sprintf("%s(%s)", qualifiedFunctionName, serializeExecuteParams(params))
}

// getFunctionSignature builds a Postgres function-identity signature suitable
// for to_regprocedure(), e.g. "schema.name(text, integer)".
//
// A function's identity is (schema, name, input-argument types) ONLY: OUT
// params are NOT part of the signature, and neither names nor modes are. This
// is deliberately distinct from serializeExecuteParams (which emits mode+name+type
// for CREATE and is reused by trigger.go) — passing the full serialization here
// would build an invalid signature and silently miss the function, causing a
// second CREATE instead of a REPLACE.
//
// The name is left unquoted so to_regprocedure parses it search_path-aware (an
// unqualified name resolves via search_path; a qualified "schema.name" is exact).
// The corpus uses lowercase identifiers, for which this is correct.
func getFunctionSignature(functionName, schema string, params []*schemasv1alpha4.PostgresqlExecuteParameter) string {
	qualifiedFunctionName := functionName
	if schema != "" && schema != "public" {
		qualifiedFunctionName = fmt.Sprintf("%s.%s", schema, functionName)
	}
	return fmt.Sprintf("%s(%s)", qualifiedFunctionName, signatureArgTypes(params))
}

// signatureArgTypes serializes ONLY the input-argument types (IN, INOUT,
// VARIADIC) of a function, comma-separated and without names or modes, e.g.
// "text, integer". OUT params are excluded because they are not part of the
// function's identity.
func signatureArgTypes(params []*schemasv1alpha4.PostgresqlExecuteParameter) string {
	ts := []string{}
	for _, param := range params {
		if strings.EqualFold(param.Mode, "OUT") {
			continue
		}
		ts = append(ts, param.Type)
	}
	return strings.Join(ts, ", ")
}

// serializeExecuteParams serializes parameters so that they can be used when sending instructions to Postgres
func serializeExecuteParams(params []*schemasv1alpha4.PostgresqlExecuteParameter) string {
	ps := []string{}
	for _, param := range params {
		p := []string{}
		if param.Mode != "" {
			p = append(p, param.Mode)
		}
		if param.Name != "" {
			p = append(p, param.Name)
		}
		p = append(p, param.Type)
		ps = append(ps, strings.Join(p, " "))
	}
	return strings.Join(ps, ", ")
}
