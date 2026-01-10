package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestInvalidateCache(t *testing.T) {
	// Create a mock SopsFS with some cached secrets
	fs := &SopsFS{
		secretsCache: make(map[string]cachedSecret),
	}

	// Add some cached entries
	fs.secretsCache["/secrets/key1"] = cachedSecret{
		value:     "value1",
		timestamp: time.Now(),
	}
	fs.secretsCache["/secrets/key2"] = cachedSecret{
		value:     "value2",
		timestamp: time.Now(),
	}

	// Verify cache has entries
	if len(fs.secretsCache) != 2 {
		t.Fatalf("Expected 2 cached entries, got %d", len(fs.secretsCache))
	}

	// Invalidate cache
	fs.invalidateCache()

	// Verify cache is empty
	if len(fs.secretsCache) != 0 {
		t.Errorf("Expected cache to be empty after invalidation, got %d entries", len(fs.secretsCache))
	}
}

func TestFileWatcherDetectsChanges(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-secrets.yaml")

	initialContent := []byte("key1: value1\nkey2: value2\n")
	if err := os.WriteFile(testFile, initialContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a mock SopsFS
	fs := &SopsFS{
		secretsPath:  testFile,
		secretsCache: make(map[string]cachedSecret),
		secretsTree:  map[string]interface{}{"key1": "value1"},
		stopWatch:    make(chan struct{}),
	}

	// Add some cached entries
	fs.secretsCache["/secrets/key1"] = cachedSecret{
		value:     "value1",
		timestamp: time.Now(),
	}

	// Start the file watcher
	if err := fs.startFileWatcher(); err != nil {
		t.Fatalf("Failed to start file watcher: %v", err)
	}
	defer fs.StopWatcher()

	// Verify cache has an entry
	if len(fs.secretsCache) != 1 {
		t.Fatalf("Expected 1 cached entry before modification, got %d", len(fs.secretsCache))
	}

	// Modify the file
	modifiedContent := []byte("key1: newvalue1\nkey2: value2\nkey3: value3\n")
	if err := os.WriteFile(testFile, modifiedContent, 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Wait for debounce + processing time
	time.Sleep(fileWatchDebounce + 200*time.Millisecond)

	// Verify cache was invalidated
	fs.mu.RLock()
	cacheSize := len(fs.secretsCache)
	fs.mu.RUnlock()

	if cacheSize != 0 {
		t.Errorf("Expected cache to be invalidated after file change, got %d entries", cacheSize)
	}
}

func TestFileWatcherDebouncing(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-secrets-debounce.yaml")

	initialContent := []byte("key1: value1\n")
	if err := os.WriteFile(testFile, initialContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a mock SopsFS
	fs := &SopsFS{
		secretsPath:  testFile,
		secretsCache: make(map[string]cachedSecret),
		secretsTree:  map[string]interface{}{"key1": "value1"},
		stopWatch:    make(chan struct{}),
	}

	// Add multiple cache entries
	for i := 0; i < 5; i++ {
		key := "/secrets/key" + string(rune('1'+i))
		fs.secretsCache[key] = cachedSecret{
			value:     "value",
			timestamp: time.Now(),
		}
	}

	// Start the file watcher
	if err := fs.startFileWatcher(); err != nil {
		t.Fatalf("Failed to start file watcher: %v", err)
	}
	defer fs.StopWatcher()

	initialCacheSize := len(fs.secretsCache)
	if initialCacheSize != 5 {
		t.Fatalf("Expected 5 initial cache entries, got %d", initialCacheSize)
	}

	// Make multiple rapid changes within the debounce window
	for i := 0; i < 5; i++ {
		content := []byte("key1: modified" + string(rune('1'+i)) + "\n")
		if err := os.WriteFile(testFile, content, 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		time.Sleep(50 * time.Millisecond) // Rapid changes within debounce window
	}

	// Wait for debounce to complete
	time.Sleep(fileWatchDebounce + 300*time.Millisecond)

	// Verify cache was cleared (debouncing should still result in cache invalidation)
	fs.mu.RLock()
	finalCacheSize := len(fs.secretsCache)
	fs.mu.RUnlock()

	if finalCacheSize != 0 {
		t.Errorf("Expected cache to be cleared after debounced changes, got %d entries", finalCacheSize)
	}
}
