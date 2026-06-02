package postgres

import (
	"testing"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"

	"github.com/stretchr/testify/assert"
)

func strptr(s string) *string { return &s }

func Test_CreatePolicyStatement(t *testing.T) {
	tenantUsing := "tenant_id = current_setting('app.tenant_id')::uuid"
	insertCheck := "tenant_id = current_setting('app.tenant_id')::uuid"

	tests := []struct {
		name              string
		tableName         string
		policy            *schemasv1alpha4.PostgresqlTablePolicy
		expectedStatement string
	}{
		{
			// ALL is the pg default command and PERMISSIVE the default mode and empty
			// roles => PUBLIC, so all three are omitted to match the catalog and keep
			// re-plans idempotent. A bare table is rendered unquoted by qualifyTableName
			// (the same convention the sibling "alter table ..." statements use).
			name:      "all permissive public using-only",
			tableName: "documents",
			policy: &schemasv1alpha4.PostgresqlTablePolicy{
				Name:  "tenant_isolation",
				Using: &tenantUsing,
			},
			expectedStatement: `create policy "tenant_isolation" on documents using (tenant_id = current_setting('app.tenant_id')::uuid)`,
		},
		{
			// SELECT (non-default command) and a single role must be emitted; the role
			// identifier is quoted via SanitizeArray.
			name:      "select with role using-only",
			tableName: "documents",
			policy: &schemasv1alpha4.PostgresqlTablePolicy{
				Name:    "tenant_read",
				Command: "SELECT",
				Roles:   []string{"app_user"},
				Using:   &tenantUsing,
			},
			expectedStatement: `create policy "tenant_read" on documents for select to "app_user" using (tenant_id = current_setting('app.tenant_id')::uuid)`,
		},
		{
			// RESTRICTIVE (non-default mode) must emit "as restrictive"; INSERT has no
			// USING, only WITH CHECK.
			name:      "insert restrictive with-check-only",
			tableName: "documents",
			policy: &schemasv1alpha4.PostgresqlTablePolicy{
				Name:       "tenant_insert",
				Command:    "INSERT",
				Permissive: "RESTRICTIVE",
				WithCheck:  &insertCheck,
			},
			expectedStatement: `create policy "tenant_insert" on documents as restrictive for insert with check (tenant_id = current_setting('app.tenant_id')::uuid)`,
		},
		{
			// UPDATE policy with both USING and WITH CHECK and multiple roles; roles
			// are each quoted and comma-joined.
			name:      "update both clauses multiple roles",
			tableName: "documents",
			policy: &schemasv1alpha4.PostgresqlTablePolicy{
				Name:      "tenant_update",
				Command:   "UPDATE",
				Roles:     []string{"app_user", "app_admin"},
				Using:     &tenantUsing,
				WithCheck: &insertCheck,
			},
			expectedStatement: `create policy "tenant_update" on documents for update to "app_user", "app_admin" using (tenant_id = current_setting('app.tenant_id')::uuid) with check (tenant_id = current_setting('app.tenant_id')::uuid)`,
		},
		{
			// Schema-qualified table must render as "schema"."table", NOT a single
			// mis-quoted identifier (the fork's prior schema-qualification bug class).
			name:      "schema-qualified table",
			tableName: "app.documents",
			policy: &schemasv1alpha4.PostgresqlTablePolicy{
				Name:  "tenant_isolation",
				Using: &tenantUsing,
			},
			expectedStatement: `create policy "tenant_isolation" on "app"."documents" using (tenant_id = current_setting('app.tenant_id')::uuid)`,
		},
		{
			// Lower-case command/permissive input must normalize the same way.
			name:      "lowercase command and permissive normalize",
			tableName: "documents",
			policy: &schemasv1alpha4.PostgresqlTablePolicy{
				Name:       "p",
				Command:    "delete",
				Permissive: "restrictive",
				Using:      &tenantUsing,
			},
			expectedStatement: `create policy "p" on documents as restrictive for delete using (tenant_id = current_setting('app.tenant_id')::uuid)`,
		},
		{
			// No clauses at all: a bare USING-less, WITH CHECK-less ALL/PERMISSIVE/PUBLIC
			// policy is valid (visible-to-all) and must render without trailing clauses.
			name:      "no clauses",
			tableName: "documents",
			policy: &schemasv1alpha4.PostgresqlTablePolicy{
				Name: "open",
			},
			expectedStatement: `create policy "open" on documents`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := CreatePolicyStatement(test.tableName, test.policy)
			assert.Equal(t, test.expectedStatement, actual)
		})
	}
}

func Test_RemovePolicyStatement(t *testing.T) {
	tests := []struct {
		name              string
		tableName         string
		policyName        string
		expectedStatement string
	}{
		{
			name:              "bare table",
			tableName:         "documents",
			policyName:        "tenant_isolation",
			expectedStatement: `drop policy if exists "tenant_isolation" on documents`,
		},
		{
			name:              "schema-qualified table",
			tableName:         "app.documents",
			policyName:        "tenant_isolation",
			expectedStatement: `drop policy if exists "tenant_isolation" on "app"."documents"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := RemovePolicyStatement(test.tableName, test.policyName)
			assert.Equal(t, test.expectedStatement, actual)
		})
	}
}

func Test_enableRowLevelSecurityStatement(t *testing.T) {
	tests := []struct {
		name              string
		tableName         string
		enable            bool
		expectedStatement string
	}{
		{
			name:              "enable bare",
			tableName:         "documents",
			enable:            true,
			expectedStatement: `alter table documents enable row level security`,
		},
		{
			name:              "disable bare",
			tableName:         "documents",
			enable:            false,
			expectedStatement: `alter table documents disable row level security`,
		},
		{
			name:              "enable schema-qualified",
			tableName:         "app.documents",
			enable:            true,
			expectedStatement: `alter table "app"."documents" enable row level security`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := enableRowLevelSecurityStatement(test.tableName, test.enable)
			assert.Equal(t, test.expectedStatement, actual)
		})
	}
}

func Test_forceRowLevelSecurityStatement(t *testing.T) {
	tests := []struct {
		name              string
		tableName         string
		force             bool
		expectedStatement string
	}{
		{
			name:              "force bare",
			tableName:         "documents",
			force:             true,
			expectedStatement: `alter table documents force row level security`,
		},
		{
			name:              "no force bare",
			tableName:         "documents",
			force:             false,
			expectedStatement: `alter table documents no force row level security`,
		},
		{
			name:              "force schema-qualified",
			tableName:         "app.documents",
			force:             true,
			expectedStatement: `alter table "app"."documents" force row level security`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := forceRowLevelSecurityStatement(test.tableName, test.force)
			assert.Equal(t, test.expectedStatement, actual)
		})
	}
}
