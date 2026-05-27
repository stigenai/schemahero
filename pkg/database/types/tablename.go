package types

import "strings"

// bareTableName returns the table name without any schema qualifier. Generated
// constraint, index, and key names are scoped to their table's schema and must
// not themselves be schema-qualified, so a name like "global.oauth_service_accounts"
// must contribute only "oauth_service_accounts" to a generated identifier
// (otherwise the dot makes the identifier invalid SQL).
func bareTableName(tableName string) string {
	if idx := strings.LastIndex(tableName, "."); idx >= 0 {
		return tableName[idx+1:]
	}
	return tableName
}
