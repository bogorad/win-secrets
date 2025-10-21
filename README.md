# Win-Secrets: A FUSE Filesystem for SOPS on Windows

win-secrets mounts a read-only virtual filesystem that exposes values from a SOPS-encrypted YAML as files, decrypting on demand through a remote SOPS keyservice over gRPC without writing plaintext to disk.[1]

### Why

- Provide file-like access to individual secrets for Windows-native tools while keeping keys centralized on a remote SOPS keyservice and avoiding local plaintext at rest.[1]
- Enable per-key lazy decryption with a small in-memory cache so reads are fast and independent of the YAML’s overall size and structure.[1]

### How it works

- On startup, the app builds a SOPS decryption engine and registers KeyServices that include a remote gRPC keyservice client (and a local client during transition), then mounts a read-only FUSE filesystem via WinFsp/cgofuse on the requested path.[1]
- Each read maps the file path to a key path, decrypts the YAML tree via the configured KeyServices, extracts the leaf value, returns it as file content, and caches it in memory for 5 minutes by default.[1]

## Requirements

- WinFsp installed, as cgofuse depends on WinFsp headers and runtime to mount a FUSE filesystem on Windows, and Go CGO must be able to find WinFsp’s fuse includes when building locally.[1]
- A reachable SOPS keyservice listening on TCP with the private keys or cloud credentials that match the recipients listed in your file’s sops metadata (for example, an age key file configured on the server).[1]

## Build

- Ensure Go is installed and that WinFsp development headers are available to CGO; if headers are not discovered automatically, point the C include path to the WinFsp fuse include directory before building.[1]
- Build the binary, optionally embedding version metadata through -ldflags so --version prints meaningful info during support and diagnostics.[2][1]

```powershell
# Optional: set version info at link time
go build -ldflags="-X 'main.Version=v0.3.0' -X 'main.Commit=abcdef1' -X 'main.Date=2025-10-21T19:00:00Z'" -o win-secrets.exe
```

## Server

- Start the SOPS keyservice on your server with a TCP listener and with keys/credentials loaded that match your SOPS file’s recipients; for example, set SOPS_AGE_KEY_FILE for age identities and run sops keyservice --network tcp --address 0.0.0.0:5000 with verbose logging.[1]

## Usage

- The program supports single-dash and double-dash flags via the standard flag package, so -keyservice and --keyservice are equivalent, and -help/--help both invoke the custom usage with an intro and version line.[3][1]

```text
win-secrets mounts a read-only virtual filesystem that exposes individual values from a SOPS-encrypted YAML file as files, decrypting on-demand via a remote SOPS keyservice over gRPC. No plaintext is written to disk; each read triggers decryption of just the requested key path and returns it as file content. [attached_file:57]

Version: <printed from build ldflags> [attached_file:57]

Usage:
  -keyservice string   SOPS keyservice address (tcp://host:port or host:port) (default "sops-keyservice.lan:5000") [attached_file:57]
  -secrets string      Path to SOPS-encrypted YAML file (default "secrets.yaml") [attached_file:57]
  -mount string        Mount point (default "/run") [attached_file:57]
  -selftest            Run a single decrypt self-test and exit [attached_file:57]
  -ks-smoketest        Ping keyservice via gRPC (expects error) and exit [attached_file:57]
  -version             Print version and exit [attached_file:57]
  -help                Show this help, intro, and version [attached_file:57][web:150]
```

- Example: run win-secrets.exe with a scheme-prefixed keyservice endpoint (tcp://host:port) so both the CLI and the embedded engine resolve the address correctly and avoid malformed DNS resolver errors from a one-slash URI.[1]

```powershell
# Mount with explicit keyservice endpoint
win-secrets.exe --keyservice tcp://sops-keyservice.lan:5000 --secrets C:\secrets\secrets.yaml --mount Z:
```

## Diagnostics

- Self-test: -selftest discovers a leaf in your YAML, logs recipients in the sops metadata, attempts one decrypt with the configured KeyServices, and exits success/failure to validate end-to-end before mounting a filesystem.[1]
- Smoke test: -ks-smoketest dials the target over gRPC and expects an “unimplemented” response from a dummy call, proving the address resolves and the server is reachable without performing decryption or requiring plaintext.[1]

## Troubleshooting

- “dns resolver: missing address” seen from the CLI or library indicates a malformed keyservice URL; use tcp://sops-keyservice.lan:5000 rather than tcp:/… or a bare value that the resolver parses incorrectly.[1]
- “Error getting data key: 0 successful groups required, got 0” means none of the file’s sops groups decrypted the data key; validate the remote keyservice is being used and that it actually holds identities or cloud credentials matching the recipients counted in diagnostics.[1]

## Implementation notes

- The decryption path uses the SOPS libraries directly, constructs a []KeyServiceClient with a remote gRPC client (and a local client during transition), and calls DecryptTree, mirroring the CLI’s keyservice semantics without shelling out to sops.exe.[1]
- The filesystem layer is implemented with cgofuse over WinFsp and exposes directories for nested YAML maps and files for leaf values, returning read-only content and default sizes until read materializes a cached plaintext string in memory.[1]

## CLI behavior

- Both single- and double-dash flags are accepted by the standard flag package, and custom Usage prints an intro paragraph and version header above the auto-generated options, with --version printing the ldflags-supplied version directly and exiting.[2][3][1]

## Repository layout

- main.go contains the FUSE filesystem, CLI flags, custom help/version, signal handling, and mounting lifecycle, and wires self-test and smoke test modes useful for operations and support.[1]
- sops_client.go owns keyservice client construction, remote gRPC connection management, SOPS DecryptTree usage, YAML parsing, recipient diagnostics, and cache-aware reads hooked by the filesystem.[1]
- keyservice/\* contains proto and generated stubs that are not imported by the executable; these files are currently unused and can be removed or kept for reference without impacting the build or runtime.[1]

## Versioning

- Build with -ldflags -X to embed Version, Commit, and Date so operators can print --version and correlate logs and binaries during support and upgrades, with dev defaults used if unset.[2][1]

## Safety and privacy

- The program never writes decrypted plaintext to disk and returns it only on read, while logs include key paths, endpoints, and timings but never secret values, preserving confidentiality during diagnostics and normal operation.[1]

[1](https://ppl-ai-file-upload.s3.amazonaws.com/web/direct-files/attachments/39244650/9fcc6c3d-a53a-46a8-b0ef-3cd865ad895d/paste.txt)
[2](https://www.digitalocean.com/community/tutorials/using-ldflags-to-set-version-information-for-go-applications)
[3](https://pkg.go.dev/flag)
