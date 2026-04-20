# Architecture

## Encrypted store

Secrets are stored in `~/.enveil` — a JSON envelope:

```json
{
  "version": 1,
  "kdf_params": { "memory": 65536, "iterations": 3, "parallelism": 4, "salt": "..." },
  "nonce": "...",
  "ciphertext": "..."
}
```

- **KDF:** Argon2id — 64 MiB RAM, 3 passes (tuneable)
- **Encryption:** ChaCha20-Poly1305 — no hardware dependency, constant-time

The ciphertext decrypts to a flat JSON map of `"item/field"` → value pairs:

```json
{ "stripe/key": "sk_live_...", "postgres/url": "postgres://..." }
```

## Agent protocol

Communication between `enveil run` and the agent uses newline-delimited JSON over a Unix domain socket:

```
→ {"op":"resolve","ref":"enveil://stripe/key"}
← {"value":"sk_live_..."}
```

`enveil run` collects all refs from the `.env` file before opening the socket, then batch-resolves them in a single connection.

If the agent is unreachable (connection refused), `enveil run` exits with a clear error and never passes reference strings through to the child process:

```
agent not running — run: enveil agent start
```

## Caller verification

The agent verifies that every connecting process is the same `enveil` binary before reading any data. Peer credentials are obtained immediately on `accept` via socket options, before any data is exchanged.

- **Linux** (`verifier_linux.go`): peer PID obtained via `SO_PEERCRED`; agent resolves `/proc/<pid>/exe` and compares its inode against the inode of `/proc/self/exe` recorded at agent startup.
- **macOS** (`verifier_darwin.go`): peer PID obtained via `LOCAL_PEERCRED`; agent resolves the executable path via `proc_pidpath` and compares against its own `os.Executable()` path.

## Reference parser

`enveil://` references are matched with:

```
^enveil://([^/]+)/([^/]+)$
```

Non-matching env var values are passed through unchanged. All refs are collected before the socket is opened so a single agent connection resolves the entire `.env` file.

## `.env` merge strategy

Values from the `.env` file are lower priority than variables already present in the shell environment. If `DATABASE_URL` is already set in the shell, the `.env` value is ignored.
