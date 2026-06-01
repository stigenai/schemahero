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

// parseSortedColumnElement parses a bare column element with optional ordering,
// e.g. "created_at DESC", "email NULLS FIRST", "name DESC NULLS LAST".
func parseSortedColumnElement(element string) types.IndexColumn {
	fields := strings.Fields(element)
	col := types.IndexColumn{}
	if len(fields) > 0 {
		col.Column = fields[0]
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
