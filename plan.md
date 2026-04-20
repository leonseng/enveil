## `enveil` — design plan

### What it does
A CLI tool that stores secrets in a local encrypted file, exposes them to child processes via `exec`, and protects the decryption key inside an agent process that communicates over a Unix domain socket. AI tools and other co-resident processes never see plaintext secrets.

---

### Architecture overview

```
enveil run -- npm run dev
       │
       ├── reads .env, finds secret://stripe/key
       ├── connects to agent socket
       ├── receives plaintext over socket
       └── exec() → npm run dev (inherits resolved env)

enveil agent start
       │
       ├── prompts master password once
       ├── derives decryption key (stays in memory only)
       ├── writes socket path to ~/.enveil-agent.env
       ├── creates /tmp/enveil-agent-$UID.sock
       └── loops: accept → verify caller → resolve → respond
```

---

### Components

**1. Encrypted store (`~/.enveil`)**
- Format: JSON envelope — `{ version, kdf_params, nonce, ciphertext }`
- KDF: Argon2id — tunable memory hardness makes offline brute-force expensive
- Encryption: ChaCha20-Poly1305 — no hardware dependency, constant-time, good Go stdlib support via `golang.org/x/crypto`
- Store schema inside ciphertext: `{ "stripe/key": "sk_live_...", "postgres/url": "..." }`

**2. Agent process**
- Spawned by `enveil agent start`, forks to background
- Holds derived key in memory — never written anywhere
- On start: checks for and cleans up any stale socket file before binding
- Listens on `/tmp/enveil-agent-$UID.sock` (mode 0600)
- Writes socket path + PID to `~/.enveil-agent.env` on successful start
- Protocol: newline-delimited JSON — `{"op":"resolve","ref":"secret://stripe/key"}` → `{"value":"sk_live_..."}`
- Exits cleanly on `SIGTERM` / `SIGHUP`, zeroes key memory on exit
- Removes socket file and `~/.enveil-agent.env` on clean exit

**3. Caller verification**
Interface: `verifier.go`
```go
type Verifier interface {
    Verify(pid uint32) (bool, error)
}
```

`verifier_linux.go` — reads `/proc/<pid>/exe`, compares inode against `/proc/self/exe` inode recorded at agent startup

`verifier_darwin.go` — resolves peer PID to executable path via `proc_pidpath` syscall, compares against agent's own `os.Executable()` path

Both platforms: peer credentials obtained via `SO_PEERCRED` (Linux) / `LOCAL_PEERCRED` (macOS) immediately on accept, before reading any data.

**4. Reference parser**
- Scans env var values for `secret://item/field` pattern
- Regex: `^secret://([^/]+)/([^/]+)$`
- Leaves non-matching values untouched
- Collects all refs before opening socket — one connection per `run` invocation, batch-resolves all refs

**5. `.env` loader**
- Use `github.com/joho/godotenv` — handles quotes, comments, multiline, `export` prefix
- Merge strategy: `.env` values are lower priority than existing shell env (don't override `DATABASE_URL` if already set in shell)

**6. Process runner**
- Resolve all refs → build merged env map → `syscall.Exec` (not `os.StartProcess`)
- `syscall.Exec` replaces the current process — no wrapper in the process tree, signals propagate naturally
- On Linux: call `prctl(PR_SET_DUMPABLE, 0)` before exec to block `/proc/<pid>/environ` reads during the brief resolution window

**7. Shell integration**

`enveil agent start` prints export lines to stdout so the calling shell can eval them:
```bash
export ENVEIL_AUTH_SOCK=/tmp/enveil-agent-1000.sock
export ENVEIL_AGENT_PID=12345
```
It also writes these to `~/.enveil-agent.env` for all future shell windows to source.

Add to `~/.zshrc` / `~/.bashrc`:
```bash
# source existing agent socket if present
if [ -f ~/.enveil-agent.env ]; then
  source ~/.enveil-agent.env
fi

# start agent if not running, eval exports into current shell
if ! enveil agent status &>/dev/null; then
  eval $(enveil agent start)
fi
```

Behaviour by scenario:
- **First terminal of the day** — agent not running, prompts for master password, starts agent, evals socket path into shell
- **Every subsequent terminal** — agent already running, sources `~/.enveil-agent.env`, no prompt
- **After reboot** — stale `~/.enveil-agent.env` exists, `agent status` fails, agent restarts cleanly, prompts once

`enveil run` behaviour when agent is unreachable:
- Connection refused → prints clear error: `agent not running — run: enveil agent start`
- Never silently passes reference strings through (strict mode)

**8. CLI interface**
```
enveil agent start          # start agent, prompts password, evals socket path
enveil agent stop           # terminate agent, zero memory, clean up socket
enveil agent status         # exit 0 if running, exit 1 if not

enveil run [--env .env] -- <cmd> [args...]

enveil secret add <item> <field>      # prompts for value
enveil secret list                    # shows item/field keys only, never values
enveil secret delete <item> <field>
enveil secret rotate <item> <field>   # re-prompts, re-encrypts
```

---

### Security properties

| Property | How achieved |
|---|---|
| Secrets never on disk in plaintext | ChaCha20-Poly1305 store, memory-only resolution |
| Key never in shell env | Agent model — only `AUTH_SOCK` path in env |
| AI tools can't read via `env` | No secrets or keys in environment |
| AI tools can't decrypt store | Key only in agent memory |
| AI tools can't use socket | Caller verification by inode (Linux) / path (macOS) |
| `/proc/<pid>/environ` on Linux | `prctl(PR_SET_DUMPABLE, 0)` before exec |
| Key zeroed on exit | Manual zero before GC — `copy(key, zeros)` in agent shutdown |
| Stale socket on reboot | Agent cleans up before binding, status check triggers fresh start |

---

### File layout

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
      verifier_linux.go   ← inode comparison
      verifier_darwin.go  ← path comparison
    store/
      store.go            ← encrypt/decrypt ~/.enveil
      kdf.go              ← Argon2id params
    env/
      loader.go           ← .env parsing + ref scanning
    run/
      run.go              ← resolve + exec
  go.mod
```

---

### Dependencies (minimal)
- `golang.org/x/crypto` — Argon2id, ChaCha20-Poly1305
- `github.com/joho/godotenv` — .env parsing
- Everything else: stdlib (`net`, `os`, `syscall`, `crypto/rand`)

---

### Build and distribution
- `go build -o enveil ./cmd/enveil` — single static binary
- Cross-compile from either platform: `GOOS=linux go build` / `GOOS=darwin go build`
- Recommended install: compile from source on target machine to avoid Gatekeeper / EDR friction
- Strip debug info: `go build -ldflags="-s -w"`

---

### Implementation order
Build and test each component independently in this order:

1. **store** — encrypt/decrypt round-trip, Argon2id KDF, file read/write
2. **agent** — socket server, protocol, key resolution, clean shutdown
3. **verify** — platform-specific caller verification plugged into agent
4. **env + run** — `.env` parsing, ref scanning, socket client, `syscall.Exec`
5. **shell integration** — `agent start` stdout + `~/.enveil-agent.env` write, stale socket handling
6. **CLI wiring** — `cobra` or `flag`-based command dispatch connecting all components
