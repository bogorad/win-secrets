# AGENTS.md

## Prerequisites

### Protocol Buffers
```bash
# Install protoc compiler
# Windows: Download from https://github.com/protocolbuffers/protobuf/releases
# Linux: sudo apt install protobuf-compiler

# Install Go plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

### WinFsp
- Download and install from https://winfsp.dev/rel/
- Required for Windows filesystem operations

## Workflow
- After each change, run `jj new -m "<summary>"` to create a new commit with a brief summary of what was changed

## Build Steps

```bash
# 1. Generate protobuf code
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    keyservice.proto

# 2. Download dependencies
go mod tidy

# 3. Build (compile only, don't run yet)
go build -o win-secrets.exe

# 4. Run tests
go test -v ./...

# 5. Run with race detection
go test -race ./...
```

## Running (After SOPS Keyservice is Ready)

```bash
# In WSL2: Start SOPS keyservice
sops keyservice --network tcp --address 127.0.0.1:5000

# In Windows: Mount filesystem
win-secrets.exe -secrets "\\wsl$\Ubuntu\home\username\secrets.yaml" -mount /run
```

## Build/Lint/Test Commands

### Build
- `go build` - Build the project
- `go run .` - Run the project directly

### Lint
- `golangci-lint run` - Run comprehensive linting
- `go vet` - Run Go's built-in static analysis
- `gofmt -d .` - Check formatting (use `gofmt -w .` to fix)

### Test
- `go test ./...` - Run all tests
- `go test -run TestName` - Run specific test function
- `go test -v` - Run tests with verbose output
- `go test -race` - Run tests with race detection

## Code Style Guidelines

### Imports
- Use `goimports` for automatic import management
- Group imports: standard library, third-party, internal
- Remove unused imports automatically

### Formatting
- Use `gofmt` for consistent formatting
- Follow Go's official formatting standards
- No semicolons (handled by compiler)
- Use tabs for indentation

### Types
- Use Go's built-in types and interfaces
- Define custom types with `type` keyword
- Use structs for data structures
- Implement interfaces implicitly

### Naming Conventions
- camelCase for package-private identifiers
- PascalCase for exported identifiers
- UPPER_SNAKE_CASE for constants
- snake_case for file names

### Error Handling
- Return errors as last return value
- Use `errors.New()` or `fmt.Errorf()` for error creation
- Check errors immediately after function calls
- Use error wrapping with `fmt.Errorf()` and `%w`

### General
- Follow Effective Go guidelines
- Keep functions small and focused
- Use goroutines and channels for concurrency
- Add comments for exported functions/types
- Use `defer` for cleanup operations