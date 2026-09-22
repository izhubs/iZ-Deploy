# izDeploy

izDeploy is a lightweight application deployment engine and daemon optimized for resource-constrained Linux servers (>=512MB RAM). It provides an embedded Model Context Protocol (MCP) server, static SHA-256 infrastructure lockfiles, and zero-downtime routing swaps for containerized workloads.

---

## Quick Setup (Under 3 Minutes)

### Phase 1: Prepare the VPS (Data Plane)

Run the unified bootstrap script on a fresh Ubuntu 24.04 LTS instance with root privileges:

```bash
# Standard bootstrap (Docker, UFW, zRAM if RAM < 2GB, daemon):
curl -sSL https://raw.githubusercontent.com/izhubs/iz-deploy/main/scripts/install-node.sh | sudo bash

# Hardened Zero-Inbound bootstrap (Tailscale SSH + Cloudflare Tunnel, Zero Open Inbound Ports):
curl -sSL https://raw.githubusercontent.com/izhubs/iz-deploy/main/scripts/install-node.sh | sudo bash -s -- \
  --tailscale-key=tskey-auth-xxxx \
  --cf-token=eyJhxxxx
```

### Phase 2: Initialize Your App Repository (Developer Machine)

Install the CLI binary:

```bash
# Build locally or download from GitHub Releases
go install github.com/izhubs/izdeploy/cmd/izdeploy@latest
```

In your application project root:

```bash
# 1. Initialize configuration (< 10 lines) and SHA-256 lockfile
izdeploy init --name "my-app" --port 3000 --image "ghcr.io/org/my-app:latest"

# 2. Validate configuration and verify lockfile integrity
izdeploy lint

# 3. Deploy to production host
izdeploy deploy
```

---

## Detailed Usage Guide

### 1. Developer CLI Commands

The `izdeploy` CLI controls the project lifecycle, configuration validation, and deployments.

| Command | Syntax | Description |
|---|---|---|
| `init` | `izdeploy init [--name <name>] [--port <port>] [--image <ref>]` | Scaffolds `.agent/izdeploy.json`, `.agent/izdeploy.lock`, `.cursor/rules/izdeploy.mdc`, and `CLAUDE.md`. |
| `lint` | `izdeploy lint [--config <path>] [--lock <path>] [--json]` | Validates schema syntax, DNS names, port bounds (1-65535), and SHA-256 infrastructure lockfile signatures. Returns exit code 0 on success, 1 on failure. |
| `deploy` | `izdeploy deploy [--local] [--force] [--strategy <zero-downtime\|recreate>]` | Dispatches container deployment. Supports `--strategy recreate` for VPS <=512MB RAM; `--force` bypasses lockfile verification. |
| `rollback` | `izdeploy rollback` | Instantly rolls back traffic to the previous healthy container version (Phase 3). |
| `db` | `izdeploy db create <mariadb\|postgres\|redis> [--name <name>]` | Provisions a low-RAM database container (96MB/128MB/32MB) with cgroup limits, local binding, and auto-generated credentials. Supports `list`, `stop`, `start`, `remove`. |
| `tunnel` | `izdeploy tunnel setup <cloudflare\|tailscale> [--dry-run]` | Configures outbound network tunnels and locks down public UFW ports. Supports `status`. |
| `menu` | `izdeploy menu` (or `izdeploy` without args on TTY) | Launches the interactive terminal management console wizard for human operators. |
| `secret` | `izdeploy secret set [KEY=VALUE...] [--file .env]` | Securely manages environment variables without exposing them in git (Phase 2). |
| `volume` | `izdeploy volume backup <name> [--s3]` | Backs up the specified Docker volume to a tar file or S3 (Phase 2). |
| `status` | `izdeploy status [--json]` | Queries the deployment status, container uptime, memory consumption, and health check state. |
| `logs` | `izdeploy logs [--app <name>] [--tail <n>] [--follow / -f] [--config <path>]` | Streams or inspects container stdout/stderr logs from daemon or local runtime. |
| `mcp` | `izdeploy mcp` | Starts the stdio-based Model Context Protocol (MCP) server for direct IDE integration. |

#### Example: Initializing a Node.js / Next.js Service

```bash
izdeploy init --name "storefront" --port 3000 --image "ghcr.io/company/storefront:v1.0.0"
```

Output:
```text
Initialized izDeploy workspace successfully:
  [+] Contract:  .agent/izdeploy.json (5 lines)
  [+] Lockfile:  .agent/izdeploy.lock (infra_hash: e01eb985)
  [+] AI Bridge: .cursor/rules/izdeploy.mdc
  [+] AI Bridge: CLAUDE.md
```

#### Example: Linting Configuration

```bash
izdeploy lint
```

Output:
```text
OK: .agent/izdeploy.json conforms to Schema v0.1
  - App Name:  storefront
  - Port:      3000
  - Image:     ghcr.io/company/storefront:v1.0.0
  - Lockfile:  .agent/izdeploy.lock (verified, infra_hash=e01eb985)
```

---

### 2. Manifest Specification (`.agent/izdeploy.json`)

The single source of truth for deployment configuration lives in `.agent/izdeploy.json`:

```json
{
  "$schema": "https://izdeploy.izhubs.com/schemas/v0.1/izdeploy.schema.json",
  "version": "0.1",
  "name": "storefront",
  "port": 3000,
  "image": "ghcr.io/company/storefront:v1.0.0",
  "env": {
    "NODE_ENV": "production",
    "CACHE_TTL": "3600"
  },
  "healthcheck": {
    "path": "/api/health",
    "interval_seconds": 15,
    "timeout_seconds": 5,
    "retries": 3
  },
  "resources": {
    "memory_limit_mb": 384,
    "cpu_quota": 0.8
  },
  "routes": [
    {
      "domain": "shop.example.com",
      "tls": "auto"
    }
  ],
  "volumes": [
    {
      "source": "storefront-uploads",
      "target": "/app/public/uploads"
    }
  ]
}
```

#### Field Constraints
- `name`: Lowercase alphanumeric and hyphens (`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`).
- `port`: Valid TCP port integer from `1` to `65535`.
- `image`: Valid OCI container reference.
- `resources.memory_limit_mb`: Minimum allocation of `32` MB.
- `resources.cpu_quota`: Float from `0.1` to `128.0` vCPUs.

---

### 3. Cryptographic Lockfile (`.agent/izdeploy.lock`)

To prevent automated AI coding agents from accidentally mutating sensitive host infrastructure (such as altering exposed ports, binding unverified volumes, or removing database mounts), izDeploy creates a SHA-256 hash manifest in `.agent/izdeploy.lock`:

```json
{
  "version": "0.1",
  "infra_hash": "e01eb985c4ad20bf",
  "locked_fields": {
    "name": "storefront",
    "port": 3000,
    "resources": "memory:384MB,cpu:0.80",
    "routes": ["shop.example.com"],
    "volumes": ["storefront-uploads:/app/public/uploads"]
  },
  "updated_at": "2026-09-19T02:59:40Z"
}
```

If an AI tool or manual edit modifies `port` without updating the lockfile, `izdeploy lint` and `izdeploy deploy` will block execution:

```text
LOCKFILE ERROR: infrastructure parameters modified without lockfile update: expected infra_hash=e01eb985, actual=0fe389ff. Use --force to override.
```

To update the lockfile after deliberate infrastructure changes:

```bash
izdeploy init --force --name "storefront" --port 8080
```

---

### 4. AI IDE Integration via Model Context Protocol (MCP)

izDeploy embeds an MCP server allowing AI editors (Cursor, Claude Code, Windsurf) to monitor production health, read logs, and trigger deployments through standard tool calls.

#### Configuring Cursor (`.cursor/mcp.json`)

Add the following to your project's `.cursor/mcp.json` or global configuration:

```json
{
  "mcpServers": {
    "izdeploy": {
      "command": "izdeploy",
      "args": ["mcp"],
      "env": {
        "IZDEPLOY_CONFIG": ".agent/izdeploy.json"
      }
    }
  }
}
```

#### Configuring Claude Code (`~/.claude/claude_code_config.json`)

```bash
claude mcp add izdeploy -- izdeploy mcp
```

#### Available MCP Tools

| Tool | Parameters | Description | Error Gating |
|---|---|---|---|
| `iz_status` | *None* | Queries application state, container uptime, memory consumption in MB, and CPU percentage. | Safe tool, always accessible. |
| `iz_deploy` | `image_tag` (string, optional) | Triggers deployment of a specific image tag after validating `.agent/izdeploy.lock`. | Fails with RFC 7807 problem details if lockfile is mismatched. |
| `iz_logs` | `tail` (int, default: 60) | Returns ring-buffered stdout/stderr logs (15 head lines + 45 tail lines) with ANSI escape sequences stripped to conserve token budget. | None. |
| `iz_restart` | *None* | Performs a graceful container restart with a 10-second drain window before fallback SIGKILL. | None. |
| `iz_env` | `variables` (key-value map) | Validates and updates environment variables in `.agent/izdeploy.json`. | Sets `agent_actionable: false` if variable keys are malformed. |

#### RFC 7807 Diagnostic Error Gating

When errors occur during deployments, izDeploy returns errors structured per RFC 7807 Problem Details:

```json
{
  "type": "urn:izdeploy:error:infra:oom",
  "title": "Out Of Memory (Linux Exit 137)",
  "status": 500,
  "detail": "Container killed by kernel OOM killer. Peak memory exceeded 384MB.",
  "error_code": "ERR_INFRA_OOM",
  "agent_actionable": false
}
```

- **`agent_actionable: true`**: Code-level issues (syntax errors, missing packages, uncaught exceptions). AI agents are permitted to inspect source code and generate patches.
- **`agent_actionable: false`**: Infrastructure-level issues (kernel OOM 137, port collision, proxy 502). AI agents are barred from modifying port numbers or deleting configuration manifests.

---

### 5. Node Daemon (`izdeploy-agent`)

The host agent runs as a systemd service managing container state and proxies:

```bash
# Check service status
sudo systemctl status izdeploy-agent

# View agent logs
sudo journalctl -u izdeploy-agent -f
```

The daemon provides internal management endpoints on `127.0.0.1:8098`:
- `GET /health`: Agent liveness probe.
- `GET /status`: Application and resource telemetry.
- `POST /deploy`: Rollout trigger for pre-built OCI images.
- `POST /restart`: Graceful restart endpoint.
- `GET /logs`: Ring-buffered log stream endpoint.
- `POST /env`: Runtime environment variable mutation.
- `POST /webhook`: Authenticated CI/CD webhook receiver endpoint.
- `POST /webhook/deploy`: Alias webhook deployment endpoint.

---

### 6. Break-Glass Watchdog (`izdeploy-watchdog`)

To eliminate the security liability of maintaining an open inbound SSH port 22 exposed to internet scanner botnets, the VPS firewall operates in a default-deny state (`ufw default deny incoming`).

The standalone `izdeploy-watchdog` process (<2MB static binary) runs every 60 seconds via `izdeploy-watchdog.timer`.
- If connectivity to the control plane is disrupted for longer than 10 consecutive minutes (10 failed health probes), the watchdog automatically inserts an emergency rate-limited SSH rule:
  ```bash
  ufw insert 1 limit 22/tcp comment 'izdeploy-break-glass'
  ```
- When connectivity is restored, the watchdog automatically revokes the emergency rule and returns the firewall to default-deny posture.

---

### 7. Automated CI/CD (GitHub Actions)

Copy `templates/github/deploy.yml` into `.github/workflows/deploy.yml` in your repository:

1. Generates an OCI container image using Nixpacks (no manual Dockerfile required).
2. Pushes the image to GitHub Container Registry (`ghcr.io`).
3. Sends a webhook notification to your izDeploy agent to execute an atomic sub-second swap.

---

### 8. Multi-App Deployment on a Single VPS

izDeploy executes multiple independent applications concurrently on a single Linux server without port collisions or routing conflicts.

#### Architectural Separation
- **State Store (PocketBase/SQLite)**: Applications maintain distinct records in the embedded database (`apps` collection). Each record isolates its container reference, image tag, internal port binding, and environment variables.
- **SNI Reverse Proxy (`kamal-proxy`)**: All external HTTP/HTTPS traffic enters through `kamal-proxy` on ports 80 and 443. The proxy inspects incoming TLS Server Name Indication (SNI) and Host headers to route traffic to the corresponding application container.
- **Automated TLS Certificates**: `kamal-proxy` handles ACME HTTP-01 and TLS-ALPN-01 challenges to provision and renew Let's Encrypt certificates independently per domain.

#### Multi-App Configuration Example

**App A (E-Commerce Storefront):**
`.agent/izdeploy.json`:
```json
{
  "name": "storefront",
  "port": 3000,
  "image": "ghcr.io/company/storefront:v1.2.0",
  "routes": ["shop.example.com"]
}
```

**App B (Internal API Service):**
`.agent/izdeploy.json`:
```json
{
  "name": "billing-api",
  "port": 8080,
  "image": "ghcr.io/company/billing-api:v2.0.1",
  "routes": ["billing.example.com"]
}
```

Both applications deploy to the same host. `kamal-proxy` routes `https://shop.example.com` to container `storefront` on port 3000 and `https://billing.example.com` to container `billing-api` on port 8080, each with its own valid SSL certificate.

---

### 9. Build & Deployment Modes

izDeploy supports four build and deployment strategies configured via the `build.mode` property in `.agent/izdeploy.json` or CLI flags:

| Mode | Target Execution Environment | Recommended Use Case |
|---|---|---|
| `github-actions` (Default) | GitHub Actions Hosted Runner | Cost-efficient VPS ($4/month, 1GB RAM). Offloads CPU and memory spikes during builds, preventing kernel OOM killer invocation. |
| `host` | VPS Node Runtime | Instances with >= 2GB–4GB RAM. Builds directly via Nixpacks or Dockerfile on the target server. |
| `local` | Developer Workstation | Fast development iterations. Compiles image locally via Docker engine (`izdeploy deploy --local`). |
| `cloud` | Centralized Build Service | Monorepos or teams utilizing dedicated remote image builders. |

#### Configuring Build Strategy in `.agent/izdeploy.json`

```json
{
  "name": "analytics-worker",
  "port": 5000,
  "image": "ghcr.io/company/analytics-worker:latest",
  "build": {
    "mode": "github-actions",
    "builder": "nixpacks"
  }
}
```

Supported `builder` options:
- `nixpacks` (default): Automatic language, runtime, and dependency detection without Dockerfiles.
- `dockerfile`: Standard multi-stage container builds using local `Dockerfile`.

---

### 10. Automated Git Webhook Deployment

The `izdeploy-agent` daemon exposes an authenticated webhook receiver for zero-downtime deployments triggered by GitHub, GitLab, or Git webhooks.

#### Endpoint Specifications
- Endpoints: `POST http://<VPS_IP>:8098/webhook` and `POST http://<VPS_IP>:8098/webhook/deploy`
- Authentication: When `IZDEPLOY_WEBHOOK_SECRET` is set in the daemon environment, all requests must provide a matching token via:
  - Header: `Authorization: Bearer <secret>`
  - Header: `X-Izdeploy-Token: <secret>`
- Security Gate: Missing or invalid credentials return `HTTP 401 Unauthorized` with RFC 7807 Problem Details (`application/problem+json`).

#### Webhook Payload Schema

```json
{
  "app": "storefront",
  "image": "ghcr.io/company/storefront:sha-9f8e7d6",
  "port": 3000,
  "mode": "pull"
}
```

#### GitHub Webhook Setup
1. In your GitHub repository, navigate to `Settings` > `Webhooks` > `Add webhook`.
2. **Payload URL**: `http://<VPS_IP>:8098/webhook`
3. **Content type**: `application/json`
4. **Secret**: Value matching `IZDEPLOY_WEBHOOK_SECRET` on your VPS.
5. **Events**: Select *Just the push event*.

---

### 11. Live Log Streaming (`izdeploy logs`)

Inspect or stream container logs for deployed services with automatic ANSI terminal escape sequence stripping.

#### CLI Syntax

```bash
izdeploy logs [--app <name>] [--tail <n>] [--follow / -f] [--config <path>] [--local]
```

#### Common Invocations

```bash
# Stream live logs continuously for a specific service
izdeploy logs -f --app storefront

# Inspect the last 100 log lines for the application defined in current directory
izdeploy logs -n 100

# Query the local offline backend directly (bypassing daemon HTTP)
izdeploy logs --local --app storefront
```

#### Resolution Logic
1. **Application Detection**: If `--app` is omitted, the CLI reads `name` from `.agent/izdeploy.json`.
2. **Connection Hierarchy**:
   - Queries `http://127.0.0.1:8098/logs?app=<name>&tail=<n>` on the host daemon.
   - Falls back to `LocalBackend` if the daemon is offline or connection is refused.
3. **Buffer Management**: In follow mode (`-f`), polls for runtime updates every 2 seconds until `SIGINT` or context cancellation.

---

### 12. Low-RAM Deployment Strategies (`strategy`)

On resource-constrained VPS instances (<= 512MB RAM), standard zero-downtime rolling deployments can trigger the Linux kernel Out-Of-Memory killer (`Exit 137`) because two instances of the application container run simultaneously during the healthcheck handover window.

izDeploy provides two deployment strategies configured via `.agent/izdeploy.json` or CLI flag:

| Strategy | Behavior | Trade-Off | Target Infrastructure |
|---|---|---|---|
| `zero-downtime` (Default) | Starts new container, verifies health check via proxy, swaps routes, terminates old container. | Requires 2x application memory during handover. | VPS with >= 1GB RAM. |
| `recreate` | Stops and removes existing container before starting the replacement container. | Brief downtime window (1-3 seconds). Zero memory spike. | Micro VPS (<= 512MB RAM). |

#### Configuration in `.agent/izdeploy.json`

```json
{
  "name": "storefront",
  "port": 3000,
  "image": "ghcr.io/company/storefront:v1.0.0",
  "strategy": "recreate"
}
```

#### CLI Override

```bash
izdeploy deploy --strategy recreate
```

---

### 13. 1-Click Micro-Database Management (`izdeploy db`)

izDeploy provisions isolated database containers tuned for micro-instances. All micro-databases bind strictly to `127.0.0.1` (never exposed to public interfaces), apply cgroup memory caps, and generate 32-byte cryptographic passwords (`crypto/rand`).

| Engine | Image Tag | RAM Cap | Local Port | Persistence Volume |
|---|---|---|---|---|
| `mariadb` | `mariadb:11.4` | 96MB | `127.0.0.1:3306` | `izdeploy-db-mariadb-data` |
| `postgres` | `postgres:16-alpine` | 128MB | `127.0.0.1:5432` | `izdeploy-db-postgres-data` |
| `redis` | `redis:7-alpine` | 32MB | `127.0.0.1:6379` | `izdeploy-db-redis-data` |

#### Commands

```bash
# Provision a database container with auto-generated credentials
izdeploy db create mariadb --name my-mariadb

# Provision a PostgreSQL container
izdeploy db create postgres

# Provision a Redis container
izdeploy db create redis

# List running and stopped database containers
izdeploy db list

# Lifecycle controls
izdeploy db stop mariadb
izdeploy db start mariadb
izdeploy db remove mariadb --force
```

---

### 14. Zero-Inbound Network Hardening & Tunnels (`izdeploy tunnel`)

For maximum server isolation, izDeploy supports running with **zero open inbound ports** (`ufw default deny incoming`). Ingress web traffic is routed via Cloudflare edge tunnels, and remote administrative access is restricted to encrypted Tailscale mesh networking.

#### Tunnel Architectures

1. **Cloudflare Tunnel (`cloudflared`)**: Establishes outbound TLS connections to Cloudflare edge nodes. Public ports 80 and 443 can be closed completely on the VPS firewall.
2. **Tailscale SSH (`tailscale`)**: Provides zero-trust mesh networking. Administrative SSH connections are authenticated through Tailscale identity providers, allowing public port 22 to be closed completely.

#### CLI Invocations

```bash
# Set up Cloudflare Tunnel (locks public HTTP/HTTPS ports)
izdeploy tunnel setup cloudflare --token "eyJh..."

# Dry-run validation of setup commands without host mutation
izdeploy tunnel setup cloudflare --token "eyJh..." --dry-run

# Set up Tailscale SSH (locks public SSH port 22)
izdeploy tunnel setup tailscale --authkey "tskey-auth-..."

# Inspect active tunnel interfaces and firewall status
izdeploy tunnel status
```

---

### 15. Interactive Terminal Management Console (`izdeploy menu`)

Human system administrators can manage services interactively through a text-based terminal wizard without memorizing CLI flags.

#### Invocation

```bash
# Explicit invocation
izdeploy menu

# Automatic invocation when running without arguments in an interactive terminal (TTY)
izdeploy
```

#### TTY Detection Logic

izDeploy checks POSIX file descriptor 0 (`os.Stdin.Fd()`) using `isatty` detection:
- **Interactive TTY (Human Terminal)**: Opens the numbered 7-option interactive menu.
- **Non-TTY Environment (AI Agent, Pipe, CI/CD Script)**: Falls back to standard POSIX help text with zero blocking prompts.

#### Menu Options

```text
========================================
       izDeploy Management Console      
========================================
1. Status & Resource Telemetry
2. Deploy Application
3. Stream Logs
4. Database Management
5. Network Tunnel Status
6. Restart Services
7. Exit
```

---

### 16. AI Agent Automation & Operations Playbook

For headless AI agents (Claude Code, Cursor, Antigravity, GitHub Actions runners), izDeploy provides deterministic non-interactive workflows.

Agents must not invoke interactive wizards or blocking commands. Refer to the complete operational specifications:
- [AI Agent Post-Deploy Operations Playbook](docs/ai_agents/post_deploy_playbook.md): Verification commands, diagnostic triages, and rollbacks.
- [RFC 7807 Diagnostics Specification](pkg/diagnostics/): Machine-readable error payloads with `agent_actionable` gating.

---

## Performance Benchmarks

izDeploy is designed to be exceptionally lightweight. See the [RAM Comparison Benchmark](benchmark/ram_comparison.md) against Coolify and Dokploy.

---

## Repository Layout

```
.
├── cmd/
│   ├── izdeploy/              # CLI binary & MCP server entrypoint
│   ├── izdeploy-agent/        # Node daemon runtime
│   └── izdeploy-watchdog/     # Break-glass rescue binary
├── internal/
│   ├── platform/              # Host system primitives (UFW, systemd)
│   └── pocketbase/            # Embedded state store and SQLite WAL migrations
├── pkg/
│   ├── contract/              # Manifest parser, schema validation, lockfile
│   ├── diagnostics/           # RFC 7807 error analyzer and token budgeting
│   ├── docker/                # Container controller (Docker Engine SDK)
│   ├── mcp/                   # Model Context Protocol stdio tools implementation
│   ├── proxy/                 # Zero-downtime reverse proxy controller (kamal-proxy)
│   └── tunnel/                # Reverse tunnel client (Chisel with Full Jitter)
├── schemas/
│   └── v0.1/                  # JSON Schema specifications
├── scripts/
│   ├── setup-vps-data-plane.sh # Host bootstrap script (Docker, UFW, user)
│   ├── setup-zram.sh          # Memory optimization (zRAM LZ4, sysctl, fallback swap)
│   └── systemd/               # Systemd service and timer unit definitions
├── templates/
│   ├── database/              # Docker templates for PostgreSQL, MySQL, Redis
│   └── github/                # Nixpacks + GHCR GitHub Actions deployment workflow
└── tests/
    ├── e2e/                   # End-to-end fixtures (nextjs-sample)
    └── integration/           # Cross-package integration test suites
```

---

## Building from Source

Requirements:
- Go >= 1.22
- Make
- GCC or Clang (optional, pure Go supported)

```bash
# Build all three binaries into bin/
make build

# Run comprehensive test suite
make test

# Run code style and static verification
make vet
make lint
```

---

## License

MIT License. See [LICENSE](LICENSE) for details.
