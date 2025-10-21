package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

type SopsClient struct {
	keyserviceAddr string
}

func NewSopsClient(addr string) (*SopsClient, error) {
	log.Printf("[SopsClient] Using Windows sops.exe with keyservice at %s", addr)

	// Verify sops.exe is available
	if _, err := exec.LookPath("sops.exe"); err != nil {
		return nil, fmt.Errorf("sops.exe not found in PATH: %w", err)
	}

	return &SopsClient{
		keyserviceAddr: addr,
	}, nil
}

func (c *SopsClient) Close() error {
	return nil
}

func (c *SopsClient) GetSecretsStructure(filePath string) (map[string]interface{}, error) {
	log.Printf("[SopsClient] Reading secrets structure from %s", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SOPS file: %w", err)
	}

	var sopsFile map[string]interface{}
	if err := yaml.Unmarshal(data, &sopsFile); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	delete(sopsFile, "sops")
	log.Printf("[SopsClient] Loaded structure with %d top-level keys", len(sopsFile))
	return sopsFile, nil
}

func (c *SopsClient) DecryptKey(ctx context.Context, filePath string, keyPath []string) (string, error) {
	log.Printf("[SopsClient] Decrypting key %v from %s", keyPath, filePath)

	// Build JSON path for --extract: ["foo"]["bar"]
	jsonPath := ""
	for _, key := range keyPath {
		jsonPath += fmt.Sprintf(`["%s"]`, key)
	}

	// Run sops.exe on Windows with keyservice
	cmd := exec.CommandContext(ctx,
		"sops.exe",
		"decrypt",
		"--enable-local-keyservice=false",
		"--keyservice", fmt.Sprintf("tcp://%s", c.keyserviceAddr),
		"--extract", jsonPath,
		filePath,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("[SopsClient] Running: sops.exe decrypt --keyservice tcp://%s --extract %s %s",
		c.keyserviceAddr, jsonPath, filePath)

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("sops.exe decrypt failed: %v, stderr: %s", err, stderr.String())
	}

	secret := strings.TrimSpace(stdout.String())
	log.Printf("[SopsClient] Successfully decrypted key %v, length: %d", keyPath, len(secret))
	return secret, nil
}

func (c *SopsClient) IsConnected() bool {
	return true
}
