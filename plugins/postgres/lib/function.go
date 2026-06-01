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

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// NOTE: the following keyword is chosen since we assume users will never need it in a function
const FunctionLogicTag = "$_SCHEMAHERO_$"

func CreateFunctionStatements(functionName string, functionSchema *schemasv1alpha4.PostgresqlFunctionSchema) []string {
	qualifiedFunctionName := getQualifiedExecuteName(functionName, functionSchema.Schema, functionSchema.Params)

	// "create or replace" makes a second apply idempotent: re-running the
	// generated migration replaces the function in place instead of erroring
	// with 42723 (function already exists). This mirrors how CreateExtensionStatements
	// always emits "if not exists".
	statement := fmt.Sprintf("create or replace function %s", qualifiedFunctionName)

	if functionSchema.Return != "" {
		statement = fmt.Sprintf("%s returns", statement)
		if functionSchema.ReturnSet {
			statement = fmt.Sprintf("%s setof", statement)
		}
		statement = fmt.Sprintf("%s %s", statement, functionSchema.Return)
	}

	// SECURITY DEFINER is a function attribute; Postgres accepts it before the
	// AS/LANGUAGE body block. Placed right after the return clause it is
	// unambiguous and keeps LANGUAGE last (as the existing tests pin).
	if functionSchema.SecurityDefiner {
		statement = fmt.Sprintf("%s security definer", statement)
	}

	statements := []string{
		// it is important to keep the function logic tags on their own respective lines
		fmt.Sprintf("%s as\n%s\n%s\n%s\nlanguage %s", statement, FunctionLogicTag, functionSchema.As, FunctionLogicTag, functionSchema.Lang),
	}

	return statements
}

func DropFunctionStatements(functionName string, functionSchema *schemasv1alpha4.PostgresqlFunctionSchema) []string {
	qualifiedFunctionName := getQualifiedExecuteName(functionName, functionSchema.Schema, functionSchema.Params)

	statements := []string{
		// "if exists" guards against a finalizer/plan re-run where the function
		// is already gone. No CASCADE (default RESTRICT): a drop that would orphan
		// a dependent (e.g. a trigger) FAILS loudly rather than silently dropping
		// dependent objects.
		fmt.Sprintf("drop function if exists %s", qualifiedFunctionName),
	}

	return statements
}
