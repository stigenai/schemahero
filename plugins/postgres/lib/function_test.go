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
	"strings"
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/stretchr/testify/assert"
)

func TestCreateFunctionStatements(t *testing.T) {
	tests := []struct {
		name     string
		function schemasv1alpha4.PostgresqlFunctionSchema
		expected []string
	}{
		{
			name: "get_user_count",
			function: schemasv1alpha4.PostgresqlFunctionSchema{
				Schema:    "public",
				Lang:      "PLpgSQL",
				Params:    []*schemasv1alpha4.PostgresqlExecuteParameter{},
				ReturnSet: false,
				Return:    "bigint",
				As: `DECLARE
  user_count bigint;
BEGIN
  SELECT COUNT(*) INTO user_count FROM users;
  RETURN user_count;
END;`,
			},
			expected: []string{
				`create or replace function get_user_count() returns bigint as
$_SCHEMAHERO_$
DECLARE
  user_count bigint;
BEGIN
  SELECT COUNT(*) INTO user_count FROM users;
  RETURN user_count;
END;
$_SCHEMAHERO_$
language PLpgSQL`,
			},
		},
		{
			name: "get_user_count_with_param",
			function: schemasv1alpha4.PostgresqlFunctionSchema{
				Schema: "public",
				Lang:   "PLpgSQL",
				Params: []*schemasv1alpha4.PostgresqlExecuteParameter{
					{
						Name: "users_table",
						Type: "text",
					},
				},
				ReturnSet: false,
				Return:    "bigint",
				As: `DECLARE
  user_count bigint;
BEGIN
  SELECT COUNT(*) INTO user_count FROM $1;
  RETURN user_count;
END;`,
			},
			expected: []string{
				`create or replace function get_user_count_with_param(users_table text) returns bigint as
$_SCHEMAHERO_$
DECLARE
  user_count bigint;
BEGIN
  SELECT COUNT(*) INTO user_count FROM $1;
  RETURN user_count;
END;
$_SCHEMAHERO_$
language PLpgSQL`,
			},
		},
		{
			name: "find_film_by_id",
			function: schemasv1alpha4.PostgresqlFunctionSchema{
				Schema: "television",
				Lang:   "PLpgSQL",
				Params: []*schemasv1alpha4.PostgresqlExecuteParameter{
					{
						Name: "p_id",
						Type: "int",
					},
				},
				ReturnSet: true,
				Return:    "film",
				As: `BEGIN
   RETURN query SELECT * FROM film WHERE film_id = p_id;
END;`,
			},
			expected: []string{
				`create or replace function television.find_film_by_id(p_id int) returns setof film as
$_SCHEMAHERO_$
BEGIN
   RETURN query SELECT * FROM film WHERE film_id = p_id;
END;
$_SCHEMAHERO_$
language PLpgSQL`,
			},
		},
		{
			name: "get_user_count_security_definer",
			function: schemasv1alpha4.PostgresqlFunctionSchema{
				Schema:          "public",
				Lang:            "PLpgSQL",
				Params:          []*schemasv1alpha4.PostgresqlExecuteParameter{},
				ReturnSet:       false,
				Return:          "bigint",
				SecurityDefiner: true,
				As: `BEGIN
  RETURN 0;
END;`,
			},
			expected: []string{
				`create or replace function get_user_count_security_definer() returns bigint security definer as
$_SCHEMAHERO_$
BEGIN
  RETURN 0;
END;
$_SCHEMAHERO_$
language PLpgSQL`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statements := CreateFunctionStatements(tt.name, &tt.function)
			assert.Equal(t, tt.expected, statements)
		})
	}
}

func TestDropFunctionStatements(t *testing.T) {
	tests := []struct {
		name     string
		function schemasv1alpha4.PostgresqlFunctionSchema
		expected []string
	}{
		{
			name: "find_film_by_id",
			function: schemasv1alpha4.PostgresqlFunctionSchema{
				Schema: "television",
				Params: []*schemasv1alpha4.PostgresqlExecuteParameter{
					{
						Name: "p_id",
						Type: "int",
					},
				},
			},
			expected: []string{
				`drop function if exists television.find_film_by_id(p_id int)`,
			},
		},
		{
			name: "foo",
			function: schemasv1alpha4.PostgresqlFunctionSchema{
				Params: []*schemasv1alpha4.PostgresqlExecuteParameter{},
			},
			expected: []string{
				`drop function if exists foo()`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statements := DropFunctionStatements(tt.name, &tt.function)
			assert.Equal(t, tt.expected, statements)
		})
	}
}

func TestGetFunctionSignature(t *testing.T) {
	tests := []struct {
		name         string
		functionName string
		schema       string
		params       []*schemasv1alpha4.PostgresqlExecuteParameter
		expected     string
	}{
		{
			name:         "no params, no schema",
			functionName: "get_user_count",
			expected:     "get_user_count()",
		},
		{
			name:         "public schema is not qualified",
			functionName: "get_user_count",
			schema:       "public",
			expected:     "get_user_count()",
		},
		{
			name:         "qualified with input params, types only",
			functionName: "find_film_by_id",
			schema:       "television",
			params: []*schemasv1alpha4.PostgresqlExecuteParameter{
				{Name: "p_id", Type: "int"},
				{Mode: "IN", Name: "p_name", Type: "text"},
			},
			expected: "television.find_film_by_id(int, text)",
		},
		{
			name:         "OUT params are excluded from the signature",
			functionName: "lookup",
			schema:       "app",
			params: []*schemasv1alpha4.PostgresqlExecuteParameter{
				{Mode: "IN", Name: "p_id", Type: "int"},
				{Mode: "OUT", Name: "o_name", Type: "text"},
				{Mode: "INOUT", Name: "io_count", Type: "bigint"},
				{Mode: "VARIADIC", Name: "tags", Type: "text[]"},
			},
			expected: "app.lookup(int, bigint, text[])",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFunctionSignature(tt.functionName, tt.schema, tt.params)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExtractDollarQuotedBody(t *testing.T) {
	tests := []struct {
		name     string
		def      string
		wantBody string
		wantOK   bool
	}{
		{
			name: "pg_get_functiondef style tag",
			def: `CREATE OR REPLACE FUNCTION test.f()
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
BEGIN
  RETURN 0;
END;
$function$`,
			wantBody: "\nBEGIN\n  RETURN 0;\nEND;\n",
			wantOK:   true,
		},
		{
			name:     "bare double-dollar tag",
			def:      "AS $$ select 1 $$",
			wantBody: " select 1 ",
			wantOK:   true,
		},
		{
			name:   "no dollar quoting",
			def:    "CREATE FUNCTION f() RETURNS void LANGUAGE sql RETURN NULL",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, ok := extractDollarQuotedBody(tt.def)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantBody, body)
			}
		})
	}
}

func TestFunctionDefinitionsEqual(t *testing.T) {
	// canonical pg_get_functiondef output for a plpgsql function (PG 16 shape)
	existing := `CREATE OR REPLACE FUNCTION test.get_user_count()
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
DECLARE
    user_count bigint;
BEGIN
    SELECT COUNT(*) INTO user_count FROM users;
    RETURN user_count;
END;
$function$
`
	body := `DECLARE
    user_count bigint;
BEGIN
    SELECT COUNT(*) INTO user_count FROM users;
    RETURN user_count;
END;`

	tests := []struct {
		name      string
		existing  string
		schema    schemasv1alpha4.PostgresqlFunctionSchema
		wantEqual bool
	}{
		{
			name:     "identical body and language is equal",
			existing: existing,
			schema: schemasv1alpha4.PostgresqlFunctionSchema{
				Lang:   "PLpgSQL",
				Return: "bigint",
				As:     body,
			},
			wantEqual: true,
		},
		{
			name:     "different body is not equal",
			existing: existing,
			schema: schemasv1alpha4.PostgresqlFunctionSchema{
				Lang:   "PLpgSQL",
				Return: "bigint",
				As:     "BEGIN\n  RETURN 1;\nEND;",
			},
			wantEqual: false,
		},
		{
			name:     "security definer mismatch is not equal",
			existing: existing,
			schema: schemasv1alpha4.PostgresqlFunctionSchema{
				Lang:            "PLpgSQL",
				Return:          "bigint",
				As:              body,
				SecurityDefiner: true,
			},
			wantEqual: false,
		},
		{
			name:     "security definer match is equal",
			existing: strings.Replace(existing, " LANGUAGE plpgsql", " LANGUAGE plpgsql\n SECURITY DEFINER", 1),
			schema: schemasv1alpha4.PostgresqlFunctionSchema{
				Lang:            "PLpgSQL",
				Return:          "bigint",
				As:              body,
				SecurityDefiner: true,
			},
			wantEqual: true,
		},
		{
			name:     "unparseable existing definition is conservatively not equal",
			existing: "CREATE FUNCTION f() RETURNS void LANGUAGE sql RETURN NULL",
			schema: schemasv1alpha4.PostgresqlFunctionSchema{
				Lang: "SQL",
				As:   "RETURN NULL",
			},
			wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := functionDefinitionsEqual(tt.existing, &tt.schema)
			assert.Equal(t, tt.wantEqual, got)
		})
	}
}
