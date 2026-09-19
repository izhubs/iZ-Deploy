# izDeploy

izDeploy is a lightweight application deployment engine and daemon optimized for resource-constrained Linux servers (>=512MB RAM). It provides an embedded Model Context Protocol (MCP) server, static SHA-256 infrastructure lockfiles, and zero-downtime routing swaps for containerized workloads.

---

## Quick Setup (Under 3 Minutes)

### Phase 1: Prepare the VPS (Data Plane)

Run the bootstrap scripts on a fresh Ubuntu 24.04 LTS instance with root privileges:

```bash
# 1. Download and run host bootstrap (installs Docker, configures UFW firewall, creates user)
curl -sSL https://raw.githubusercontent.com/izhubs/izdeploy/main/scripts/setup-vps-data-plane.sh | sudo bash

# 2. Configure 512MB zRAM LZ4 swap and kernel memory parameters (prevents OOM on 1GB VPS)
curl -sSL https://raw.githubusercontent.com/izhubs/izdeploy/main/scripts/setup-zram.sh | sudo bash

# 3. Enable and start the agent daemon
sudo systemctl enable --now izdeploy-agent
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
| `deploy` | `izdeploy deploy [--local] [--force]` | Dispatches container deployment. The `--local` flag builds via local Docker engine and pushes to registry; `--force` bypasses lockfile verification. |
| `status` | `izdeploy status [--json]` | Queries the deployment status, container uptime, memory consumption, and health check state. |
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
