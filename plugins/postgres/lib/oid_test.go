package postgres

import "testing"

func Test_stripOIDClass(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "basic",
			value: `'11'::integer`,
			want:  "11",
		},
		{
			name:  "empty",
			value: `''::character varying`,
			want:  "",
		},
		{
			name:  "identity",
			value: `testing`,
			want:  "testing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripOIDClass(tt.value); got != tt.want {
				t.Errorf("stripOIDClass() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_formatColumnDefault(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		// already-quoted (Table-spec embedded-quote convention) -> as-is
		{"quoted string", "'pending'", "'pending'"},
		{"quoted empty literal", "''", "''"},
		// function calls -> as-is (quoting them corrupts the value)
		{"gen_random_uuid", "gen_random_uuid()", "gen_random_uuid()"},
		{"now", "now()", "now()"},
		// introspected `'x'::type` cast -> stripped then re-quoted
		{"oid-class text", "'pending'::text", "'pending'"},
		// bare plain string -> quoted
		{"bare word", "pending", "'pending'"},
		{"numeric-looking kept safe by quoting", "11", "'11'"},
		{"empty string -> empty quotes", "", "''"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatColumnDefault(tt.raw); got != tt.want {
				t.Errorf("formatColumnDefault(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
