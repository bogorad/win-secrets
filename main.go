package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/winfsp/cgofuse/fuse"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

// Populated at link time via -ldflags, with sane defaults for dev
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func init() {
	// Custom help that includes a one-paragraph intro and version
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(),
			"win-secrets mounts a read-only virtual filesystem that exposes individual values from a SOPS-encrypted YAML file as files, decrypting on-demand via a remote SOPS keyservice over gRPC. No plaintext is written to disk; each read triggers decryption of just the requested key path and returns it as file content.\n\n",
		)
		fmt.Fprintf(flag.CommandLine.Output(), "Version: %s (commit %s, date %s)\n\n", Version, Commit, Date)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n")
		flag.PrintDefaults()
	}
}

var (
	ErrNotFound = errors.New("not found")
	ErrInternal = errors.New("internal error")
)

type cachedSecret struct {
	value     string
	timestamp time.Time
}

const (
	secretCacheTTL     = 5 * time.Minute
	cacheCleanupPeriod = 10 * time.Minute
)

type SopsFS struct {
	fuse.FileSystemBase
	sopsClient   *SopsClient
	secretsPath  string
	secretsTree  map[string]interface{}
	secretsCache map[string]cachedSecret
	mu           sync.RWMutex
}

func NewSopsFS(sopsClient *SopsClient, secretsPath string) (*SopsFS, error) {
	fs := &SopsFS{
		sopsClient:   sopsClient,
		secretsPath:  secretsPath,
		secretsCache: make(map[string]cachedSecret),
	}

	if err := fs.refreshSecretsStructure(); err != nil {
		return nil, fmt.Errorf("failed to load secrets structure: %w", err)
	}

	go fs.cacheCleanupLoop()

	return fs, nil
}

func (fs *SopsFS) cacheCleanupLoop() {
	ticker := time.NewTicker(cacheCleanupPeriod)
	defer ticker.Stop()

	for range ticker.C {
		fs.mu.Lock()
		now := time.Now()
		for path, cached := range fs.secretsCache {
			if now.Sub(cached.timestamp) > secretCacheTTL {
				delete(fs.secretsCache, path)
				log.Printf("[CacheCleanup] Removed expired cache entry for %s", path)
			}
		}
		fs.mu.Unlock()
	}
}

func (fs *SopsFS) refreshSecretsStructure() error {
	structure, err := fs.sopsClient.GetSecretsStructure(fs.secretsPath)
	if err != nil {
		return err
	}

	fs.mu.Lock()
	fs.secretsTree = structure
	fs.mu.Unlock()

	log.Printf("[SopsFS] Loaded secrets structure with %d top-level keys", len(structure))
	return nil
}

func (fs *SopsFS) navigateToPath(keyPath []string) (interface{}, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var current interface{} = fs.secretsTree
	for _, key := range keyPath {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func (fs *SopsFS) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	log.Printf("[Getattr] path=%s", path)

	if path == "/" {
		stat.Mode = fuse.S_IFDIR | 0555
		return 0
	}

	if path == "/secrets" {
		stat.Mode = fuse.S_IFDIR | 0555
		return 0
	}

	if !strings.HasPrefix(path, "/secrets/") {
		return -2 // ENOENT
	}

	keyPath := parseSopsKeyPath(path)
	if keyPath == nil {
		return -2 // ENOENT
	}

	node, exists := fs.navigateToPath(keyPath)
	if !exists {
		return -2 // ENOENT
	}

	if _, isMap := node.(map[string]interface{}); isMap {
		stat.Mode = fuse.S_IFDIR | 0555
		return 0
	}

	stat.Mode = fuse.S_IFREG | 0444
	stat.Size = 4096

	log.Printf("[Getattr] File %s exists, using default size", path)
	return 0
}

func (fs *SopsFS) Open(path string, flags int) (int, uint64) {
	log.Printf("[Open] path=%s flags=%d", path, flags)

	if !strings.HasPrefix(path, "/secrets/") {
		return -2, 0 // ENOENT
	}

	keyPath := parseSopsKeyPath(path)
	if keyPath == nil {
		return -2, 0 // ENOENT
	}

	node, exists := fs.navigateToPath(keyPath)
	if !exists {
		return -2, 0 // ENOENT
	}

	if _, isMap := node.(map[string]interface{}); isMap {
		return -21, 0 // EISDIR
	}

	return 0, 0
}

func (fs *SopsFS) Release(path string, fh uint64) int {
	log.Printf("[Release] path=%s fh=%d", path, fh)
	return 0
}

func (fs *SopsFS) Read(path string, buff []byte, ofst int64, fh uint64) int {
	log.Printf("[Read] path=%s offset=%d size=%d", path, ofst, len(buff))

	if !strings.HasPrefix(path, "/secrets/") {
		return -2 // ENOENT
	}

	secret, err := fs.readSecret(path)
	if err != nil {
		log.Printf("[Read] Error reading secret: %v", err)
		return -5 // EIO
	}

	data := []byte(secret)
	if ofst >= int64(len(data)) {
		return 0
	}

	n := copy(buff, data[ofst:])
	log.Printf("[Read] Returning %d bytes", n)
	return n
}

func (fs *SopsFS) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	log.Printf("[Readdir] path=%s", path)

	fill(".", nil, 0)
	fill("..", nil, 0)

	if path == "/" {
		fill("secrets", &fuse.Stat_t{Mode: fuse.S_IFDIR | 0555}, 0)
		return 0
	}

	if path == "/secrets" {
		fs.mu.RLock()
		defer fs.mu.RUnlock()

		for name, value := range fs.secretsTree {
			var mode uint32
			if _, isMap := value.(map[string]interface{}); isMap {
				mode = fuse.S_IFDIR | 0555
			} else {
				mode = fuse.S_IFREG | 0444
			}
			fill(name, &fuse.Stat_t{Mode: mode}, 0)
		}
		return 0
	}

	if !strings.HasPrefix(path, "/secrets/") {
		return -2 // ENOENT
	}

	keyPath := parseSopsKeyPath(path)
	if keyPath == nil {
		return -2 // ENOENT
	}

	node, exists := fs.navigateToPath(keyPath)
	if !exists {
		return -2 // ENOENT
	}

	m, ok := node.(map[string]interface{})
	if !ok {
		return -20 // ENOTDIR
	}

	for name, value := range m {
		var mode uint32
		if _, isMap := value.(map[string]interface{}); isMap {
			mode = fuse.S_IFDIR | 0555
		} else {
			mode = fuse.S_IFREG | 0444
		}
		fill(name, &fuse.Stat_t{Mode: mode}, 0)
	}

	return 0
}

func (fs *SopsFS) Opendir(path string) (int, uint64) {
	log.Printf("[Opendir] path=%s", path)

	if path == "/" || path == "/secrets" {
		return 0, 0
	}

	if !strings.HasPrefix(path, "/secrets/") {
		return -2, 0 // ENOENT
	}

	keyPath := parseSopsKeyPath(path)
	if keyPath == nil {
		return -2, 0 // ENOENT
	}

	node, exists := fs.navigateToPath(keyPath)
	if !exists {
		return -2, 0 // ENOENT
	}

	if _, isMap := node.(map[string]interface{}); !isMap {
		return -20, 0 // ENOTDIR
	}

	return 0, 0
}

func (fs *SopsFS) Releasedir(path string, fh uint64) int {
	log.Printf("[Releasedir] path=%s fh=%d", path, fh)
	return 0
}

func parseSopsKeyPath(filePath string) []string {
	parts := strings.Split(strings.TrimPrefix(filePath, "/"), "/")
	if len(parts) < 2 || parts[0] != "secrets" {
		return nil
	}

	keys := parts[1:]
	for i, k := range keys {
		keys[i] = strings.TrimSuffix(k, ".yaml")
		keys[i] = strings.TrimSuffix(keys[i], ".txt")
	}
	return keys
}

func (fs *SopsFS) readSecret(path string) (string, error) {
	keyPath := parseSopsKeyPath(path)
	if keyPath == nil {
		return "", ErrNotFound
	}

	if fs.sopsClient == nil {
		return "", ErrInternal
	}

	fs.mu.RLock()
	if cached, ok := fs.secretsCache[path]; ok {
		if time.Since(cached.timestamp) < secretCacheTTL {
			fs.mu.RUnlock()
			log.Printf("[ReadSecret] Cache HIT for %s", path)
			return cached.value, nil
		}
	}
	fs.mu.RUnlock()

	log.Printf("[ReadSecret] Cache MISS for %s, decrypting...", path)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	secret, err := fs.sopsClient.DecryptKey(ctx, fs.secretsPath, keyPath)
	if err != nil {
		return "", err
	}

	fs.mu.Lock()
	fs.secretsCache[path] = cachedSecret{
		value:     secret,
		timestamp: time.Now(),
	}
	fs.mu.Unlock()

	log.Printf("[ReadSecret] Cached decrypted secret for %s", path)
	return secret, nil
}

// findTestKeyPath finds a suitable key path for self-testing by looking for the first leaf value
func findTestKeyPath(secretsPath string) []string {
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		log.Printf("[SelfTest] Cannot read secrets file for test path discovery: %v", err)
		return nil
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		log.Printf("[SelfTest] Cannot parse secrets file for test path discovery: %v", err)
		return nil
	}

	// Remove sops metadata
	delete(root, "sops")

	// Find the first leaf path
	var path []string
	if findLeafPath(root, &path) {
		return path
	}

	log.Printf("[SelfTest] Could not find any leaf values in secrets structure")
	return nil
}

// findLeafPath recursively finds the first leaf path in the structure
func findLeafPath(node interface{}, currentPath *[]string) bool {
	switch v := node.(type) {
	case map[string]interface{}:
		for key, value := range v {
			*currentPath = append(*currentPath, key)
			if findLeafPath(value, currentPath) {
				return true
			}
			*currentPath = (*currentPath)[:len(*currentPath)-1]
		}
		return false
	default:
		// Found a leaf
		return true
	}
}

func main() {
	keyserviceAddr := flag.String("keyservice", "sops-keyservice.lan:5000", "SOPS keyservice address (tcp://host:port or host:port)")
	secretsPath := flag.String("secrets", "secrets.yaml", "Path to SOPS-encrypted YAML file")
	mountPoint := flag.String("mount", "/run", "Mount point")
	selfTest := flag.Bool("selftest", false, "Run a single decrypt self-test and exit")
	ksSmoke := flag.Bool("ks-smoketest", false, "Ping keyservice via gRPC (expects error) and exit")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	// Handle --version early
	if *showVersion {
		fmt.Printf("win-secrets %s (commit %s, date %s)\n", Version, Commit, Date)
		return
	}

	if *ksSmoke {
		if err := configureSOPSKeyservice(*keyserviceAddr); err != nil {
			log.Fatalf("Failed to configure SOPS keyservice: %v", err)
		}

		endpoint := os.Getenv("SOPS_KEYSERVICE")
		target := strings.TrimPrefix(endpoint, "tcp://")

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		cc, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err != nil {
			log.Fatalf("[Smoke] Dial: %v", err)
		}
		defer cc.Close()

		// Test gRPC connectivity by making a call to a non-existent service
		// This will fail with "unimplemented" if the server is responding
		err = cc.Invoke(ctx, "/test.TestService/TestMethod", nil, nil)
		if err == nil {
			log.Fatalf("[Smoke] unexpected success - server should not implement test service")
		}

		// Check if it's an "unimplemented" error (good) or connection error (bad)
		if strings.Contains(err.Error(), "unimplemented") || strings.Contains(err.Error(), "Unimplemented") {
			log.Printf("[Smoke] OK - server responded with unimplemented (gRPC server is running)")
		} else {
			log.Fatalf("[Smoke] FAIL - unexpected error: %v", err)
		}
		return
	}

	if *selfTest {
		if err := configureSOPSKeyservice(*keyserviceAddr); err != nil {
			log.Fatalf("Failed to configure SOPS keyservice: %v", err)
		}
		LogSopsRecipients(*secretsPath)
		sc, err := NewSopsClient(*keyserviceAddr)
		if err != nil {
			log.Fatalf("Failed to create SOPS client: %v", err)
		}
		defer sc.Close()

		// Try to find a test key path - for now, use a hardcoded path or find first leaf
		testPath := findTestKeyPath(*secretsPath)
		if testPath == nil {
			log.Fatalf("[SelfTest] Could not find a suitable test key path")
		}

		val, err := sc.DecryptKey(context.Background(), *secretsPath, testPath)
		if err != nil {
			log.Fatalf("[SelfTest] FAIL: %v", err)
		}
		log.Printf("[SelfTest] OK: %d bytes", len(val))
		return
	}

	// Remove the error check since we now have a default
	log.Printf("Starting SOPS Secrets Filesystem Proxy")
	log.Printf("Keyservice: %s", *keyserviceAddr)
	log.Printf("Secrets file: %s", *secretsPath)
	log.Printf("Mount point: %s", *mountPoint)

	if err := configureSOPSKeyservice(*keyserviceAddr); err != nil {
		log.Fatalf("Failed to configure SOPS keyservice: %v", err)
	}

	sopsClient, err := NewSopsClient(*keyserviceAddr)
	if err != nil {
		log.Fatalf("Failed to create SOPS client: %v", err)
	}
	defer sopsClient.Close()

	fs, err := NewSopsFS(sopsClient, *secretsPath)
	if err != nil {
		log.Fatalf("Failed to create filesystem: %v", err)
	}

	host := fuse.NewFileSystemHost(fs)
	host.SetCapReaddirPlus(true)

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Received shutdown signal, unmounting...")
		host.Unmount()
	}()

	log.Printf("Mounting filesystem at %s", *mountPoint)

	ret := host.Mount(*mountPoint, []string{"-o", "volname=SOPS Secrets"})
	if !ret {
		log.Fatal("Mount failed")
	}

	log.Println("Filesystem unmounted successfully")
}
