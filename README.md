# enveil

A CLI tool that stores secrets in a local encrypted file and injects them into child processes — without ever placing them in plaintext on disk or in environment variables visible to co-resident processes (including AI coding assistants).

```
STRIPE_KEY=secret://stripe/key   # in your .env
```

```bash
enveil run -- npm run dev        # npm run dev inherits STRIPE_KEY resolved to the real value
```

---

## How it works

```
enveil run -- npm run dev
       │
       ├── reads .env, finds secret://stripe/key
       ├── connects to agent socket
       ├── receives plaintext over socket (never touches disk)
       └── syscall.Exec() → npm run dev (inherits resolved env)

enveil agent start
       │
       ├── prompts master password once
       ├── derives decryption key (stays in memory only)
       ├── creates /tmp/enveil-agent-$UID.sock
       └── loops: accept → verify caller → resolve → respond
```

The **agent** holds the decryption key in memory. No other process can use the socket — the agent verifies that every caller is the same `enveil` binary (by executable inode on Linux, by path on macOS) before reading any data.

---

## Security properties

| Property | How achieved |
|---|---|
| Secrets never on disk in plaintext | ChaCha20-Poly1305 encrypted store |
| Key never in shell environment | Agent model — only socket path in env |
| AI tools can't read secrets via `env` | No secrets or keys in environment |
| AI tools can't decrypt the store | Decryption key only in agent memory |
| AI tools can't use the socket | Caller verification by executable inode / path |
| `/proc/<pid>/environ` blocked on Linux | `prctl(PR_SET_DUMPABLE, 0)` before exec |
| Key zeroed on agent exit | `copy(key, zeros)` in shutdown handler |
| Stale socket on reboot | Cleaned up before binding; `agent status` triggers fresh start |

---

## Installation

Requires Go 1.22+.

```bash
git clone https://github.com/leonzalion/enveil
cd enveil
go build -ldflags="-s -w" -o enveil ./cmd/enveil
sudo mv enveil /usr/local/bin/   # or anywhere on your PATH
```

> **Recommended:** compile from source on the target machine to avoid Gatekeeper / EDR friction.

---

## Quick start

### 1. Start the agent

```bash
eval $(enveil agent start)
# Master password: ••••••••
# export ENVEIL_AUTH_SOCK=/tmp/enveil-agent-1000.sock
# export ENVEIL_AGENT_PID=12345
```

### 2. Add secrets

```bash
enveil secret add stripe key
# Master password: ••••••••
# Value for stripe/key: ••••••••••••••••

enveil secret add postgres url
# Master password: ••••••••
# Value for postgres/url: ••••••••••••••••
```

### 3. Reference them in your `.env`

```bash
STRIPE_KEY=secret://stripe/key
DATABASE_URL=secret://postgres/url
PORT=3000
```

### 4. Run your app

```bash
enveil run -- npm run dev
enveil run --env .env.production -- ./myapp
```

---

## Shell integration

Add to `~/.zshrc` or `~/.bashrc` so the agent starts automatically and every new terminal window picks it up:

```bash
# Source existing agent socket if present
if [ -f ~/.enveil-agent.env ]; then
  source ~/.enveil-agent.env
fi

# Start agent if not running, eval exports into current shell
if ! enveil agent status &>/dev/null; then
  eval $(enveil agent start)
fi
```

**Behaviour by scenario:**
- **First terminal of the day** — agent not running; prompts for master password once; starts agent; evals socket path into shell
- **Every subsequent terminal** — agent already running; sources `~/.enveil-agent.env`; no prompt
- **After reboot** — stale `~/.enveil-agent.env` exists; `agent status` fails; agent restarts cleanly; prompts once

---

## CLI reference

```
enveil agent start          Start the agent (prompts for master password)
enveil agent stop           Stop the running agent
enveil agent status         Exit 0 if running, 1 if not

enveil run [--env .env] -- <cmd> [args...]
                            Resolve secrets and exec cmd

enveil secret add <item> <field>      Prompt for value and add to store
enveil secret list                    List all item/field keys (never values)
enveil secret delete <item> <field>   Remove a secret
enveil secret rotate <item> <field>   Re-prompt and re-encrypt a secret
```

---

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

---

## Dependencies

| Package | Purpose |
|---|---|
| `golang.org/x/crypto` | Argon2id, ChaCha20-Poly1305 |
| `github.com/joho/godotenv` | `.env` file parsing |
| `github.com/spf13/cobra` | CLI |
| `golang.org/x/sys` | `prctl` on Linux |
| `golang.org/x/term` | Masked password prompts |

---

## License

MIT
