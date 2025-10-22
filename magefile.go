//go:build mage

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

var (
	ldflags = fmt.Sprintf(`-w -s -X 'main.Version=%s' -X 'main.Commit=%s' -X 'main.Date=%s'`,
		version(), commit(), date())
)

func version() string {
	v, _ := sh.Output("git", "describe", "--tags", "--always", "--dirty")
	if v == "" {
		return "dev"
	}
	return v
}

func commit() string {
	c, _ := sh.Output("git", "rev-parse", "--short", "HEAD")
	if c == "" {
		return "none"
	}
	return c
}

func date() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// Build for current platform
func Build() error {
	mg.Deps(Deps)
	env := map[string]string{"CGO_ENABLED": "0"}
	return sh.RunWith(env, "go", "build", "-ldflags", ldflags, "-o", "win-secrets.exe")
}

// BuildWindowsAmd64 builds for Windows x64
func BuildWindowsAmd64() error {
	mg.Deps(Deps)
	env := map[string]string{
		"CGO_ENABLED": "0",
		"GOOS":        "windows",
		"GOARCH":      "amd64",
	}
	return sh.RunWith(env, "go", "build", "-ldflags", ldflags, "-o", "win-secrets-windows-amd64.exe")
}

// BuildWindowsArm64 builds for Windows ARM64
func BuildWindowsArm64() error {
	mg.Deps(Deps)
	env := map[string]string{
		"CGO_ENABLED": "0",
		"GOOS":        "windows",
		"GOARCH":      "arm64",
	}
	return sh.RunWith(env, "go", "build", "-ldflags", ldflags, "-o", "win-secrets-windows-arm64.exe")
}

// BuildAll builds for all Windows platforms
func BuildAll() {
	mg.Deps(BuildWindowsAmd64, BuildWindowsArm64)
}

// Test runs tests
func Test() error {
	return sh.Run("go", "test", "-v", "./...")
}

// Clean removes build artifacts
func Clean() error {
	os.Remove("win-secrets.exe")
	os.Remove("win-secrets-windows-amd64.exe")
	os.Remove("win-secrets-windows-arm64.exe")
	return nil
}

// Deps downloads dependencies
func Deps() error {
	return sh.Run("go", "mod", "download")
}

// Version prints version info
func Version() {
	fmt.Printf("Version: %s\nCommit: %s\nDate: %s\n", version(), commit(), date())
}
