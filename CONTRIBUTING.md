# Contributing

## File layout

```
enveil/
  cmd/
    enveil/
      main.go
  internal/
    agent/
      agent.go            ← socket server, session loop, env file write/cleanup
      protocol.go         ← request/response types
    verify/
      verifier.go         ← interface
      verifier_linux.go   ← inode comparison via /proc
      verifier_darwin.go  ← path comparison via proc_pidpath
    store/
      store.go            ← encrypt/decrypt ~/.enveil
      kdf.go              ← Argon2id params
    env/
      loader.go           ← .env parsing + ref scanning
    run/
      run.go              ← resolve + exec
  go.mod
```

## Development

```bash
# Build for development (with debug symbols)
CGO_ENABLED=0 go build -o enveil ./cmd/enveil

# Run all tests
go test ./...

# Agent protocol tests only
go test ./internal/agent/...

# CLI integration tests only
go test ./cmd/enveil/...
```

> **Note:** The installation build uses `-ldflags="-s -w"` to strip debug symbols for a smaller binary. The development build omits these flags so stack traces remain readable.

## Dependencies

| Package | Purpose |
|---|---|
| `golang.org/x/crypto` | Argon2id, ChaCha20-Poly1305 |
| `github.com/joho/godotenv` | `.env` file parsing |
| `github.com/spf13/cobra` | CLI |
| `golang.org/x/sys` | `prctl` on Linux |
| `golang.org/x/term` | Masked password prompts |

## Cross-compilation

```bash
GOOS=linux  CGO_ENABLED=0 go build -ldflags="-s -w" -o enveil-linux  ./cmd/enveil
GOOS=darwin CGO_ENABLED=0 go build -ldflags="-s -w" -o enveil-darwin ./cmd/enveil
```
