# Win-Secrets: SOPS Secrets Filesystem for Windows

Win-Secrets is a FUSE-based filesystem that provides transparent access to SOPS-encrypted secrets on Windows systems. It bridges the gap between WSL2-stored encrypted secrets and native Windows applications by mounting secrets as a virtual filesystem.

## Description

This project enables Windows applications to securely access encrypted secrets stored in SOPS (Secrets OPerationS) files without exposing plaintext credentials. The system works by:

1. Reading the structure of encrypted secrets from a SOPS YAML file
2. Presenting this structure as a directory hierarchy through FUSE
3. On-demand decryption of individual secrets when accessed
4. Caching decrypted values for performance

The filesystem appears as a read-only mount point where each secret becomes a file that can be read by any Windows application, with decryption happening transparently in the background.

## Architecture & Logic

### Core Components

- **SopsFS**: Main FUSE filesystem implementation that handles file operations
- **SopsClient**: Interface to SOPS decryption via Windows `sops.exe`
- **KeyService**: gRPC service definitions for SOPS keyservice integration
- **Cache System**: In-memory caching of decrypted secrets with TTL

### How It Works

1. **Initialization**:
   - Parses the SOPS file structure to build a virtual directory tree
   - Establishes connection to SOPS keyservice running in WSL2
   - Mounts the FUSE filesystem at the specified mount point

2. **File Operations**:
   - `Getattr`: Determines if paths represent directories or files based on SOPS structure
   - `Readdir`: Lists secrets as files/directories in the virtual filesystem
   - `Open/Read`: Triggers decryption of specific secrets on-demand

3. **Decryption Flow**:

   ```
   Application Request → FUSE → SopsFS → SopsClient → sops.exe → KeyService → Decrypted Secret
   ```

4. **Caching**:
   - Decrypted secrets cached for 5 minutes to reduce decryption overhead
   - Automatic cleanup of expired cache entries every 10 minutes
   - Thread-safe cache operations with read/write mutexes

### Security Model

- Secrets are never stored in plaintext on disk
- Decryption happens in memory only when requested
- SOPS keyservice handles cryptographic operations
- Filesystem is read-only to prevent accidental exposure

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

### SOPS

- Install SOPS in WSL2: https://github.com/getsops/sops
- Ensure `sops.exe` is available in Windows PATH

## Setup Instructions

### 1. Build the Project

```bash
# Generate protobuf code
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    keyservice.proto

# Download dependencies
go mod tidy

# Build
go build -o win-secrets.exe

# Run tests
go test -v ./...
```

### 2. Prepare Secrets File

Create a SOPS-encrypted `secrets.yaml` file in WSL2 with your secrets:

```yaml
# secrets.yaml (encrypted with SOPS)
database:
  host: encrypted_host_value
  password: encrypted_password_value
api_keys:
  github: encrypted_github_token
  aws: encrypted_aws_key
```

### 3. Start SOPS Keyservice

In WSL2 terminal:

```bash
# Start the keyservice on localhost:5000
sops keyservice --network tcp --address 127.0.0.1:5000
```

### 4. Mount the Filesystem

In Windows Command Prompt or PowerShell:

```cmd
# Mount secrets filesystem
win-secrets.exe -secrets "\\wsl$\Ubuntu\home\username\secrets.yaml" -mount Z:
```

### 5. Access Secrets

Once mounted, secrets are accessible as files:

```cmd
# Read database password
type Z:\database\password

# List available secrets
dir Z:\database\
```

## Usage Examples

### Command Line Access

```bash
# Database configuration
set DB_PASSWORD=Z:\database\password
set DB_HOST=Z:\database\host

# API keys
set GITHUB_TOKEN=Z:\api_keys\github
```

### Application Integration

```python
# Python example
with open('Z:\\api_keys\\github', 'r') as f:
    token = f.read().strip()
```

### Docker Integration

```dockerfile
# Dockerfile
COPY Z:\\database\\password /app/db_password
```

## Configuration Options

- `-keyservice`: SOPS keyservice address (default: localhost:5000)
- `-secrets`: Path to encrypted secrets file in WSL2
- `-mount`: Mount point for the virtual filesystem

## Performance Considerations

- First access to a secret requires decryption (may take 1-2 seconds)
- Subsequent accesses within 5 minutes use cached values
- Cache TTL and cleanup intervals are configurable in the source code
- Decryption timeout is set to 10 seconds per operation

## Troubleshooting

### Common Issues

1. **Mount fails**: Ensure WinFsp is installed and keyservice is running
2. **Access denied**: Check WSL2 path format (`\\wsl$\Distro\path\to\file`)
3. **Decryption errors**: Verify SOPS configuration and key access
4. **Slow performance**: Check keyservice connectivity and cache settings

### Debug Logging

The application provides detailed logging. Check the console output for:

- Filesystem operations
- Decryption attempts
- Cache hits/misses
- Error conditions

## Development

### Code Structure

```
├── main.go           # FUSE filesystem implementation
├── sops_client.go    # SOPS decryption client
├── main_test.go      # Unit tests
├── keyservice/       # Protocol buffer definitions
│   ├── keyservice.proto
│   ├── keyservice.pb.go
│   └── keyservice_grpc.pb.go
└── AGENTS.md         # Build and development notes
```

### Testing

```bash
# Run unit tests
go test -v

# Run with race detection
go test -race
```

### Building

```bash
# Development build
go build -o win-secrets.exe

# Optimized build
go build -ldflags="-s -w" -o win-secrets.exe
```

## Security Notes

- The mounted filesystem is read-only
- Secrets are decrypted on-demand and cached temporarily
- Ensure proper access controls on the mount point
- Regularly rotate encryption keys per your security policy
- Monitor access patterns for anomalous activity

## License

This project is open source. Please refer to the license file for details.
