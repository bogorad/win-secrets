# Justfile for win-secrets

# Default recipe - show available commands
default:
    @just --list

# Build the application
build:
    go build -o win-secrets.exe

# Build with version info
build-release:
    go build -ldflags "-w -s -X 'main.Version={{`git describe --tags --always --dirty`}}' -X 'main.Commit={{`git rev-parse --short HEAD`}}' -X 'main.Date={{`date -u +%Y-%m-%dT%H:%M:%SZ`}}'" -o win-secrets.exe

# Run unit tests only (no service interaction)
test-unit:
    go test -v -timeout 30s ./...

# Stop the scheduled task, kill all processes, test, then restart
test:
    @echo "Stopping win-secrets scheduled task..."
    -@schtasks //end //tn "win-secrets" 2>nul || echo "Task not running or not found (continuing)"
    @echo "Terminating all win-secrets.exe processes..."
    -@taskkill //F //IM win-secrets.exe 2>nul || echo "No win-secrets.exe processes found"
    @echo "Running unit tests..."
    go test -v -timeout 30s ./...
    @echo "Copying win-secrets.exe to c:/bin/..."
    @cp -f ./win-secrets.exe c:/bin/win-secrets.exe
    @echo "Restarting win-secrets scheduled task..."
    @schtasks //run //tn "win-secrets"

# Run all tests including race detection
test-race:
    go test -race -v -timeout 60s ./...

# Clean build artifacts
clean:
    -@rm -f win-secrets.exe win-secrets-windows-*.exe

# Download dependencies
deps:
    go mod download
    go mod tidy

# Run with default settings (for local testing)
run:
    go run . -keyservice sops-keyservice.lan:5000 -secrets secrets.yaml -mount /run
