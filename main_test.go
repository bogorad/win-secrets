package main

import (
	"testing"
)

func TestParseSopsKeyPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple top-level key",
			input:    "/secrets/vaultwarden_admin_token",
			expected: []string{"vaultwarden_admin_token"},
		},
		{
			name:     "nested key",
			input:    "/secrets/postgres/admin_pass",
			expected: []string{"postgres", "admin_pass"},
		},
		{
			name:     "deeply nested key",
			input:    "/secrets/aws/hosted_zone_id_bogorad_eu",
			expected: []string{"aws", "hosted_zone_id_bogorad_eu"},
		},
		{
			name:     "key with .yaml extension",
			input:    "/secrets/postgres/test_pass.yaml",
			expected: []string{"postgres", "test_pass"},
		},
		{
			name:     "key with .txt extension",
			input:    "/secrets/codeium_config.txt",
			expected: []string{"codeium_config"},
		},
		{
			name:     "invalid path - no secrets prefix",
			input:    "/other/path",
			expected: nil,
		},
		{
			name:     "invalid path - empty",
			input:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSopsKeyPath(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Expected length %d, got %d", len(tt.expected), len(result))
				return
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("At index %d: expected %s, got %s", i, tt.expected[i], result[i])
				}
			}
		})
	}
}

// Note: NewSopsFS test requires real keyservice running - skipped in unit tests
