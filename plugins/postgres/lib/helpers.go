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
