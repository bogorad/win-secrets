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
)

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
	sopsClient     *SopsClient
	secretsWSLPath string
	secretsTree    map[string]interface{}
	secretsCache   map[string]cachedSecret
	mu             sync.RWMutex
}

func NewSopsFS(sopsClient *SopsClient, secretsWSLPath string) (*SopsFS, error) {
	fs := &SopsFS{
		sopsClient:     sopsClient,
		secretsWSLPath: secretsWSLPath,
		secretsCache:   make(map[string]cachedSecret),
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
	structure, err := fs.sopsClient.GetSecretsStructure(fs.secretsWSLPath)
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

	secret, err := fs.sopsClient.DecryptKey(ctx, fs.secretsWSLPath, keyPath)
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

func main() {
	keyserviceAddr := flag.String("keyservice", "localhost:5000", "SOPS keyservice address")
	secretsPath := flag.String("secrets", "\\\\wsl$\\nixos\\persist\\nix-config\\nx-ago-testing\\secrets\\secrets.yaml", "Path to secrets.yaml in WSL2")
	mountPoint := flag.String("mount", "/run", "Mount point")
	flag.Parse()

	// Remove the error check since we now have a default
	log.Printf("Starting SOPS Secrets Filesystem Proxy")
	log.Printf("Keyservice: %s", *keyserviceAddr)
	log.Printf("Secrets file: %s", *secretsPath)
	log.Printf("Mount point: %s", *mountPoint)

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
