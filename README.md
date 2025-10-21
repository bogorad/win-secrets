# Win-Secrets: A FUSE Filesystem for SOPS on Windows

Win-Secrets mounts a read-only virtual filesystem that exposes individual values from a SOPS-encrypted YAML file as files. It is designed to bridge the gap between secrets managed by SOPS and native Windows applications that expect to read credentials from files, decrypting secrets on-demand via a remote SOPS keyservice over gRPC.

## Architecture & Logic

Win-Secrets provides transparent, on-demand decryption of secrets without ever writing plaintext data to disk.

### Core Components

- **SopsFS (`main.go`)**: The main FUSE filesystem implementation, built using `cgofuse`. It handles filesystem operations like `Getattr`, `Readdir`, and `Read`.
- **SopsClient (`sops_client.go`)**: A client responsible for communicating with a remote SOPS keyservice. It handles the decryption of the secrets file.
- **KeyService (`keyservice.proto`)**: gRPC service definitions for integrating with the SOPS keyservice.
- **In-Memory Cache**: A caching system that stores decrypted secrets for a short duration (5 minutes) to enhance performance and reduce redundant decryption calls.

### How It Works

1.  **Initialization**: Upon starting, the application connects to the specified SOPS keyservice. It then parses the structure of the SOPS-encrypted YAML file to build a virtual directory tree in memory.
2.  **Mounting**: The virtual directory tree is mounted as a read-only filesystem on the specified mount point (e.g., a drive letter like `Z:`).
3.  **File Access**: When a user or application attempts to read a file from the virtual filesystem:
    - The filesystem intercepts the read request.
    - It checks its in-memory cache for a valid (non-expired) decrypted value. If a hit occurs, it returns the cached secret.
    - If the secret is not in the cache, the `SopsClient` sends the entire encrypted file to the SOPS keyservice for decryption.
    - The client receives the decrypted data, extracts the specific value corresponding to the requested file path, and returns it to the user.
    - The newly decrypted secret is stored in the cache for subsequent requests.

This entire process is transparent to the end-user or application, which simply sees a directory structure and reads files as usual.

### Security Model

- **No Plaintext on Disk**: Secrets are never stored in plaintext on the host machine's disk.
- **In-Memory Decryption**: Decryption occurs entirely in memory and only for the requested operation.
- **Read-Only Access**: The mounted filesystem is strictly read-only, preventing any accidental modification or exposure of the secret structure.
- **Centralized Key Management**: All cryptographic operations are delegated to the SOPS keyservice, ensuring that private keys remain on a secure, centralized server.

## Prerequisites

### 1. WinFsp

WinFsp is required for creating FUSE filesystems on Windows.

- **Download and install from:** [https://winfsp.dev/rel/](https://winfsp.dev/rel/)

### 2. Protocol Buffers (`protoc`)

The `protoc` compiler is needed to generate Go code from the `.proto` definition files.

- **Installation:**
  - **Windows:** Download the binary from the [Protocol Buffers GitHub releases page](https://github.com/protocolbuffers/protobuf/releases).
  - **Linux (for cross-compilation/dev):** `sudo apt install protobuf-compiler`
- **Go Plugins:**
  ```bash
  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
  ```

### 3. SOPS Keyservice

A running SOPS keyservice is required on your network to handle decryption requests. Refer to the [SOPS documentation](https://github.com/getsops/sops) for setup instructions.

## Setup Instructions

### 1. Build the Project

Clone the repository and run the following commands to build the executable:

````bash
# 1. Generate Go code from the protobuf definition
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    keyservice/keyservice.proto

# 2. Download Go module dependencies
go mod tidy

# 3. Build the executable
go build -o win-secrets.exe```

### 2. Prepare Your Secrets File
Ensure you have a SOPS-encrypted YAML file. For example, `secrets.yaml`:
```yaml
# This file is encrypted with SOPS
wifi-bruc:
  main:
    ssid: ENC[...]
    pass: ENC[...]
  guest:
    ssid: ENC[...]
    pass: ENC[...]
sops:
  # sops metadata
  ...
````

### 3. Start the SOPS Keyservice

On your server (e.g., in WSL2 or a Linux machine), start the SOPS keyservice:`bash
sops keyservice --network tcp --address 0.0.0.0:5000`

### 4. Mount the Filesystem

From a command prompt or PowerShell on your Windows machine, run `win-secrets.exe` with the appropriate flags:

```cmd
win-secrets.exe -secrets "C:\path\to\your\secrets.yaml" -keyservice "your-server-ip:5000" -mount "Z:"
```

Your secrets are now accessible as a virtual drive at `Z:`.

## Usage and Configuration

### Accessing Secrets

Once mounted, you can access your secrets as if they were regular files. Given the example `secrets.yaml` above, the directory structure on the `Z:` drive would be:

```
Z:
└── wifi-bruc
    ├── main
    │   ├── ssid
    │   └── pass
    └── guest
        ├── ssid
        └── pass
```

You can read a secret using standard command-line tools or any application:

```cmd
type Z:\wifi-bruc\main\pass
```

### Command-Line Flags

The application can be configured with the following flags:

| Flag            | Description                                                        | Default                    |
| --------------- | ------------------------------------------------------------------ | -------------------------- |
| `-keyservice`   | Address of the SOPS keyservice.                                    | `sops-keyservice.lan:5000` |
| `-secrets`      | Path to the SOPS-encrypted YAML file.                              | `secrets.yaml`             |
| `-mount`        | The mount point for the virtual filesystem (e.g., a drive letter). | `/run`                     |
| `-selftest`     | Runs a single decryption test against the keyservice and exits.    | `false`                    |
| `-ks-smoketest` | Pings the keyservice to verify gRPC connectivity and exits.        | `false`                    |
| `-version`      | Prints the application version and exits.                          | `false`                    |

## Troubleshooting and Diagnostics

The application provides detailed logging to the console, showing filesystem operations, cache status (hit/miss), and decryption activity. For diagnosing connection issues, use the built-in test flags:

- **Keyservice Smoke Test**: Verify that the `win-secrets` application can establish a gRPC connection to the keyservice.

  ```cmd
  win-secrets.exe -ks-smoketest -keyservice "your-server-ip:5000"
  ```

  A successful test will log `[Smoke] OK`.

- **Decryption Self-Test**: Perform a full end-to-end test by decrypting the first available secret in your `secrets.yaml` file. This confirms that the keyservice is running and has the correct keys to decrypt your file.
  ```cmd
  win-secrets.exe -selftest -secrets "C:\path\to\secrets.yaml" -keyservice "your-server-ip:5000"
  ```
  A successful test will log `[SelfTest] OK`.

## Development

### Code Structure

```
.
├── main.go               # Main application and FUSE filesystem logic
├── sops_client.go        # Client for interacting with the SOPS keyservice
├── main_test.go          # Unit tests for path parsing
├── keyservice/
│   ├── keyservice.proto  # Protobuf definition for the keyservice
│   └── ...               # Generated Go files for gRPC
├── go.mod                # Go module definition
└── README.md             # This file
```

### Testing

Run the unit tests included in the project:

````bash
go test -v ./...
```To check for race conditions during development:
```bash
go test -race ./...
````
