# Centaurx Architecture

## Scope

This document describes how Centaurx is built, how the parts interact, and what is stored on disk. It is a
technical architecture and specification reference, not a user guide.

## High-level topology

Centaurx is a multi-surface UI around Codex. The core service is transport-agnostic and is hosted by a
single server process. A separate runner process executes Codex and shell commands, typically inside a
container, and streams structured events over gRPC.

```
+---------------------+          +------------------------------+          +--------------------+
| Web UI (HTTP+SSE)   | <------> | centaurx serve (HTTP/SSH)    | <------> | centaurx runner    |
| Android app (HTTP)  |          |   - core service             |  gRPC/UDS| (codex exec + cmd) |
+---------------------+          |   - auth + sessions          |          +--------------------+
                                 |   - repo + tab state         |
+---------------------+          |   - event fanout             |
| SSH TUI (gliderlabs)| <------> |   - runner provider          |
+---------------------+          +------------------------------+
```

Key properties:
- Tabs are per user and shared across all frontends (web, SSH, Android).
- A tab maps to a repo and a long-lived Codex session (thread id).
- Output is formatted once in the core and delivered to all UIs via events.

## Codebase map

Top-level responsibilities by package:
- `cmd/centaurx`: CLI entrypoints for `serve`, `runner`, `bootstrap`, `build`, and user management.
- `centaurx` (root package): compositor that wires core service, auth store, runner provider, HTTP and SSH.
- `core`: transport-agnostic service (tabs, buffers, sessions, persistence, repo resolver, runner orchestration).
- `schema`: shared types for requests/responses, events, markers, and constants.
- `internal/command`: slash command parsing and execution (/new, /help, /status, etc).
- `internal/repo`: repo creation, discovery, cloning in the host filesystem.
- `internal/persist`: per-user tab state persistence to JSON files.
- `internal/sshkeys`: encrypted git SSH key store per user.
- `internal/sshagent`: per-user SSH agent server (SSH_AUTH_SOCK) backed by stored keys.
- `internal/userhome`: per-user home setup and template rendering for .codex/config.toml.
- `internal/codex`: `codex exec` runner and JSONL stream parsing.
- `internal/runnergrpc`: gRPC server/client for runner transport.
- `internal/runnercontainer`: per-tab runner container management.
- `internal/shipohoy`: container runtime abstraction (Podman, containerd adapters).
- `httpapi`: HTTP API, static web UI assets, SSE hub, session store.
- `sshserver`: SSH server and TUI rendering.
- `android/`: Android client (Jetpack Compose, OkHttp, SSE).

## Configuration and bootstrap

### Config files
- Main config file: YAML, versioned by `config_version` (current version is 4).
- Runner-only config: YAML for `centaurx runner` when it is launched as a standalone daemon.

Defaults (from `internal/appconfig`):
- HTTP: `:27480`
- SSH: `:27422`
- Repo root: `~/.centaurx/repos`
- State dir: `~/.centaurx/state`
- Users file: `~/.centaurx/users.json`

### Bootstrap
`centaurx bootstrap` writes:
- Host config (`config.yaml`) with host paths.
- Container bundle files (config-for-container.yaml, containerfiles, podman.yaml, docker-compose.yaml).
- Skel directory for per-user home initialization.

Templates in `bootstrap/files/skel` are copied to each user home and rendered using Go templates. Files ending
in `.tmpl` are templated and written without the suffix.

## Runtime lifecycle

### Startup
`centaurx serve` does the following:
1. Loads config and validates runner configuration.
2. Ensures per-user home directories exist (based on the auth store). This creates `.codex/config.toml` and
   `.ssh/known_hosts` if missing.
3. Ensures the encrypted SSH key store exists.
4. Creates a per-user SSH agent manager.
5. Initializes the runner provider (container-based by default).
6. Builds the composite server with HTTP and SSH listeners.
7. Starts HTTP and SSH servers and waits for shutdown.

A banner logo is printed when `LOG_MODE` is not `json` or `structured` and `--no-banner` is not set.

### Shutdown
The composite server stops HTTP and SSH, and runner containers are cleaned up through the provider.

## Core service model

### Tabs and sessions
Each tab stores:
- Repo reference (name + path).
- Codex model selection.
- Session id (thread id from `codex exec --json`).
- Status: idle, running, or stopped.
- Buffer and history.

Tabs are stored in a per-user map with a stable ordering list for UI rendering. Tabs and ordering are
persisted to disk.

### Buffers and scrolling
`core/buffer.go` stores scrollback lines with a scroll offset relative to the bottom:
- `scroll_offset = 0` means at the bottom (auto-follow).
- New lines increase the scroll offset if the user is scrolled up.
- Buffers are capped (`buffer_max_lines` in config).

A separate system buffer holds output not tied to a tab (help output, errors, shell commands without a tab).

### History
Each tab has a history buffer (default 200 entries). The HTTP and Android UIs use `/api/history` to
provide prompt history navigation.

### Persistence
Per-user snapshots are stored as JSON under `state_dir`:
- `state_dir/<user>.json` stores tabs, order, buffers, theme, and history.
- Scroll offsets are preserved.
- Tab status is not persisted; tabs reload as idle on restart.

## Command routing

`internal/command` handles all slash commands and `!` shell commands. It runs in the server process and
operates through the core service and runner provider.

Examples (not exhaustive):
- `/new <repo|git-url>`: create or open repo and open a tab.
- `/listrepos`: list repos under the user's repo root.
- `/help`: print command help with marker-aware formatting.
- `/status`: print active session status and usage if available.
- `/version`: print version info with themed markers.
- `/codexauth`: upload auth.json (web and Android) or paste content (SSH TUI).
- `! <cmd>`: run shell command through the runner.

Command output is appended to the active tab buffer or the system buffer if no tab is active.

## Codex execution pipeline

### Runner interface
`core.Runner` is a transport-agnostic interface. In production it is backed by gRPC and a runner container.

- `Run`: starts `codex exec` with JSON output.
- `RunCommand`: runs shell commands (used for `!`, git summaries, and repo operations).
- `Signal`: HUP/TERM/KILL support for stopping sessions.

### JSONL event handling
`internal/codex`:
- Launches `codex exec` with `--json` and reads JSONL from stdout.
- Stderr is mapped into `schema.EventError` events so errors surface in the same stream.
- JSONL parsing keeps raw event payloads for forward compatibility.

`core.service` consumes events:
- Captures `thread_id` to update the tab session id.
- Updates usage on `turn.completed` events.
- Formats events into user-facing lines with `internal/format`.
- Appends lines to buffers and emits events to sinks.

Command execution output uses a separate stream and is also appended to buffers, with stderr lines prefixed
by `schema.StderrMarker`.

### Output markers and inline markdown
`schema/output.go` defines control-byte markers used by the renderer to annotate lines:
- Agent messages, reasoning, commands, help, and version/about lines are tagged.
- UIs interpret markers to apply styles and limited markdown parsing.

`internal/markdown` supports a subset of inline markdown: `**bold**`, `*italic*`, and `` `code` ``.

## Runner process and gRPC

### Runner gRPC API
The runner exposes a gRPC service over a Unix domain socket (no TCP). The API supports:
- Exec / ExecResume: run Codex and stream structured events.
- RunCommand: run shell commands and stream stdout/stderr.
- SignalSession: send HUP/TERM/KILL to an active run.
- Ping / GetUsage: keepalive and usage fetch.

The server records a per-run `run_id` to route signals and events.

### Keepalive
The runner server tracks the last Ping. If the server misses a configured number of pings, it exits.
The runner provider treats repeated ping failures as a signal to close the tab and clean up the container.

## Runner containers

Runner containers are managed per tab by `internal/runnercontainer.Provider`:
- One container per (user, tab).
- Read-only root filesystem with tmpfs for `/tmp`, `/run`, `/var/run`, and `/var/tmp`.
- Mounted paths:
  - Host repo root for the user -> container repo root (default `/repos/<user>`).
  - Host home root for the user -> container home (`/centaurx`).
  - Per-tab runner socket dir -> container socket dir.
  - Per-user SSH agent dir -> container agent dir.

Environment inside the container:
- `HOME=/centaurx` and XDG dirs under `/centaurx`.
- `SSH_AUTH_SOCK` points at the per-user agent socket (if available).
- `GIT_SSH_COMMAND` is set to:
  - `StrictHostKeyChecking=accept-new`
  - `UserKnownHostsFile=/centaurx/.ssh/known_hosts`
  - `PreferredAuthentications=publickey`
  - `IdentityAgent` pointing at the agent socket
  - optional `-vvv` and `LogLevel=DEBUG3` when `runner.git_ssh_debug` is enabled

The provider sweeps idle containers and removes the socket directory when a tab is closed.

## Repo management and git cloning

Repo roots are per user:
- `repo_root/<user>/...` on the host.
- Mapped to `runner.repo_root/<user>/...` in the runner container.

Repo operations:
- Repo create: `git init` and `git switch -c centaurx` (fallback to checkout).
- Clone: `git clone` using the per-user SSH agent.
- Git status summary is collected via runner commands at the start of each Codex run.

## Authentication and user management

### HTTP auth
- Users are stored in `users.json` with `bcrypt` password hashes and TOTP secrets.
- `POST /api/login` validates username, password, and TOTP code.
- Session cookie is `HttpOnly` and uses an in-memory session store with TTL.

### SSH auth
- SSH login uses public key + TOTP.
- Public key must match a login key stored in the user record.
- The server then prompts for TOTP via keyboard-interactive auth.

### User management
`centaurx users` manages:
- Add/remove users.
- Change password or rotate TOTP.
- Manage SSH login keys.
- Rotate git SSH keys.

## SSH key management (git access)

Centaurx hosts git SSH keys for each user (separate from login keys):
- Encrypted private keys are stored under `state_dir/ssh/keys/<user>/key.enc`.
- Public keys are stored under `state_dir/ssh/keys/<user>/key.pub`.
- A root key for encryption lives in `state_dir/ssh/keys.bundle`.
- Encryption is handled via `pkt.systems/kryptograf` and `keymgmt`.

The server exposes these keys via a per-user SSH agent socket. The agent implements the
`session-bind@openssh.com` extension to avoid failures with OpenSSH agent extensions.

## Per-user home and Codex auth

Each user has a home directory under `state_dir/home/<user>`:
- `.codex/config.toml` is created from a template if missing.
- `.codex/auth.json` is created by `/codexauth` (web, Android) or by paste in the SSH TUI.
- `.ssh/known_hosts` is created empty to support `StrictHostKeyChecking=accept-new`.

`/codexauth` saves the auth JSON only if it is valid JSON. The file is writable so the Codex CLI can
refresh tokens in place.

## HTTP API and Web UI

HTTP endpoints (all under `/api`):
- `POST /login`, `POST /logout`
- `GET /me`
- `GET /tabs`, `POST /tabs/activate`
- `POST /prompt`
- `GET /buffer`, `GET /system`, `GET/POST /history`
- `POST /chpasswd`
- `POST /codexauth`
- `GET /stream` (SSE)

SSE stream behavior:
- Immediately sends a snapshot event with tabs, buffers, system buffer, and theme.
- Replays missed events based on `Last-Event-ID`.
- Streams output, system, and tab events as they occur.

Static assets live in `httpapi/assets`. The server injects base href and UI buffer limits into the
served HTML at runtime.

## SSH TUI

The SSH TUI uses an alternate screen and a custom renderer. It supports:
- Tab switching and scrollback.
- Prompt editing with history navigation.
- Status spinner for running commands.
- `/codexauth` paste mode: content ends on a blank line or Ctrl-D, then saves auth.json.

Events are delivered from the core service via an in-process event bus.

## Android app

The Android app mirrors the web UI behavior:
- Jetpack Compose UI, OkHttp for HTTP requests.
- SSE via OkHttp EventSource with `Last-Event-ID` replay.
- Persistent cookie jar backed by DataStore for HTTP session cookies.
- Endpoint stored in DataStore (default `http://localhost:27480`).

If SSE disconnects, the app falls back to polling `/api/buffer` and `/api/system` to keep output
current and shows a warning status banner.

The terminal view renders output markers and inline markdown similarly to the web UI.

## Logging and audit

Logging uses `pslog` and includes context fields (user, tab, session id, repo). Command auditing is
enabled by default and can be disabled via config or `--disable-audit-trails`.

## Failure modes and recovery

- SSE reconnect: clients can reconnect and replay events with `Last-Event-ID`.
- Runner keepalive: missed pings trigger cleanup of runner containers.
- Idle sweep: unused runner containers are removed after a configurable timeout.
- State recovery: tab buffers and history are reloaded from disk on first access.

## Testing surfaces

Centaurx includes multiple test tiers:
- `internal/integration` covers HTTP, SSH, runner container behavior, and git SSH flows.
- Runner runtime integration tests validate container exec markers.
- Android UI tests live under `android/app/src/androidTest`.

Run the standard Go quality gates before release:
- `go test ./...`
- `go vet ./...`
- `golint ./...`
- `golangci-lint run ./...`
