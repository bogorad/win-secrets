package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/getsops/sops/v3/aes"
	sopscommon "github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/keyservice"
	yamlstore "github.com/getsops/sops/v3/stores/yaml"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

type SopsClient struct {
	keyserviceAddr string
	conn           *grpc.ClientConn
	services       []keyservice.KeyServiceClient
}

// configureSOPSKeyservice normalizes the endpoint for diagnostics and smoke tests
func configureSOPSKeyservice(addr string) error {
	endpoint := addr
	if !(strings.HasPrefix(addr, "tcp://") || strings.HasPrefix(addr, "unix://")) {
		endpoint = "tcp://" + addr
	}

	// For smoke test compatibility, still set the environment
	if err := os.Setenv("SOPS_KEYSERVICE", endpoint); err != nil {
		return err
	}

	log.Printf("[Diag] Normalized keyservice endpoint: %s", endpoint)
	return nil
}

// sopsMeta represents the SOPS metadata structure for diagnostics
type sopsMeta struct {
	Sops struct {
		Age []struct {
			Recipient string `yaml:"recipient"`
		} `yaml:"age"`
		Pgp []struct {
			FP string `yaml:"fp"`
		} `yaml:"pgp"`
		KMS []struct {
			ARN string `yaml:"arn"`
		} `yaml:"kms"`
		GCPCMS []struct {
			ResourceID string `yaml:"resource_id"`
		} `yaml:"gcp_kms"`
		AzureKV []struct {
			Name string `yaml:"name"`
		} `yaml:"azure_kv"`
		Vault []any `yaml:"hc_vault"`
	} `yaml:"sops"`
}

// LogSopsRecipients reads the SOPS file and logs the key recipients for diagnostics
func LogSopsRecipients(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[Diag] read %s: %v", path, err)
		return
	}
	var m sopsMeta
	if err := yaml.Unmarshal(b, &m); err != nil {
		log.Printf("[Diag] parse %s: %v", path, err)
		return
	}
	// Summarize without values
	log.Printf("[Diag] sops.age recipients=%d, pgp=%d, kms=%d, gcp_kms=%d, azure_kv=%d, vault=%d",
		len(m.Sops.Age), len(m.Sops.Pgp), len(m.Sops.KMS), len(m.Sops.GCPCMS), len(m.Sops.AzureKV), len(m.Sops.Vault))
}

func NewSopsClient(addr string) (*SopsClient, error) {
	log.Printf("[SopsClient] Using remote SOPS keyservice at %s", addr)

	// Normalize: strip tcp:// for grpc.Dial, which expects host:port
	target := strings.TrimPrefix(addr, "tcp://")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("dial keyservice %q: %w", target, err)
	}

	// Remote gRPC keyservice client
	remote := keyservice.NewKeyServiceClient(conn)

	// Include both local and remote clients during transition
	// TODO: Remove local client once remote-only is desired
	svcs := []keyservice.KeyServiceClient{keyservice.NewLocalClient(), remote}

	log.Printf("[SopsClient] Configured %d KeyServices: local + remote gRPC to %s", len(svcs), addr)

	return &SopsClient{keyserviceAddr: addr, conn: conn, services: svcs}, nil
}

func (c *SopsClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
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
	start := time.Now()
	log.Printf("[SopsClient] Decrypting key %v from %s", keyPath, filePath)

	// 1) Load encrypted YAML into a SOPS tree
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read encrypted file: %w", err)
	}

	ys := &yamlstore.Store{}
	tree, err := ys.LoadEncryptedFile(data)
	if err != nil {
		return "", fmt.Errorf("load encrypted file: %w", err)
	}

	// 2) Decrypt the tree using the remote+local keyservices (matches CLI flow)
	_, err = sopscommon.DecryptTree(sopscommon.DecryptTreeOpts{
		Tree:        &tree,
		KeyServices: c.services,
		IgnoreMac:   false,
		Cipher:      aes.NewCipher(),
	})
	if err != nil {
		log.Printf("[SopsClient] decrypt failed after %s: %v (KeyServices=%d)",
			time.Since(start), err, len(c.services))
		return "", fmt.Errorf("sops decrypt failed: %w", err)
	}
	log.Printf("[SopsClient] decrypt ok in %s", time.Since(start))

	// 3) Emit plaintext YAML and extract the requested key
	plaintext, err := ys.EmitPlainFile(tree.Branches)
	if err != nil {
		return "", fmt.Errorf("emit plaintext: %w", err)
	}

	var root any
	if err := yaml.Unmarshal(plaintext, &root); err != nil {
		return "", fmt.Errorf("parse decrypted YAML: %w", err)
	}

	cur := root
	for _, k := range keyPath {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("path error at %q", k)
		}
		v, ok := m[k]
		if !ok {
			return "", fmt.Errorf("key not found: %v", keyPath)
		}
		cur = v
	}

	switch v := cur.(type) {
	case string:
		return v, nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func (c *SopsClient) IsConnected() bool {
	return true
}
