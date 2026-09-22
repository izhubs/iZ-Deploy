# [AI AGENT PLAYBOOK]: Post-Deployment Operations & Management via izDeploy

**Purpose**: Standard operating procedure for AI coding agents (Cursor, Windsurf, Claude Code, Antigravity) to inspect, configure, and maintain applications and infrastructure post-deployment on Linux VPS nodes.

**Context**: AI agents must execute non-interactive, deterministic commands. Never invoke interactive terminal prompts or menu wizards (e.g. `izdeploy menu`) as they require TTY stdin and will block execution in headless environments.

---

## 1. Health Inspection & Resource Monitoring

### Command
```bash
izdeploy status
```

### Actionable Rules
- **Healthy Container**: Status shows `running`, `exit_code: 0`, `oom_killed: false`.
- **OOM Termination (Exit Code 137)**:
  - If `oom_killed: true` or exit code is 137, the container exceeded its cgroup memory limit.
  - Action: Check host free RAM (`free -m`). If the VPS is <=512MB, verify that `strategy: "recreate"` is enabled in `.agent/izdeploy.json` so rolling updates do not double memory consumption. If necessary, adjust `resources.memory_mb` in `.agent/izdeploy.json`.
- **Application Crash (Exit Code 1)**:
  - Inspect application logs immediately.

---

## 2. Log Analysis & Diagnostics

### Command
```bash
izdeploy logs --tail 100
```

### Actionable Rules
- Parse stderr for database connection refused errors, missing environment variables, or port binding failures.
- Resolve database connection errors by verifying the connection DSN against active databases via `izdeploy db list`.

---

## 3. Database Provisioning & Attachment

When an application requires a database engine on the same VPS, provision a low-RAM tuned container:

### Commands
```bash
# Provision low-RAM MariaDB (96MB RAM cap, 48MB buffer pool)
izdeploy db create mariadb --name app-db

# Provision low-RAM PostgreSQL (128MB RAM cap, 32MB shared buffers)
izdeploy db create postgres --name app-db

# Provision low-RAM Redis (32MB RAM cap, 24MB allkeys-lru)
izdeploy db create redis --name app-cache
```

### Actionable Rules
- Capture the emitted `Connection DSN` and `.env` snippet from the output.
- Inject the credentials into the application using `izdeploy secret set KEY=VALUE` or update `.agent/izdeploy.json`.
- All provisioned databases bind strictly to `127.0.0.1` to prevent external network exposure.

---

## 4. Secret & Environment Variable Updates

### Commands
```bash
# Set individual secret key-value pairs
izdeploy secret set DATABASE_URL=mysql://app:pass@127.0.0.1:3306/app

# Load multiple variables from a local env file
izdeploy secret set --file .agent/secrets.env
```

### Actionable Rules
- Never commit plain-text credentials to `.agent/izdeploy.json` or Git repositories.
- Use `izdeploy secret set` to update runtime environment securely.

---

## 5. Network Hardening & Zero-Inbound Tunnels

To eliminate open inbound ports on the host VPS post-deployment:

### Cloudflare Tunnel (Close Web Ports 80 and 443)
```bash
# Preview changes first
izdeploy tunnel setup cloudflare --token <CF_TOKEN> --dry-run

# Execute provisioning on Linux host
izdeploy tunnel setup cloudflare --token <CF_TOKEN>
```
*Effect*: Registers `cloudflared.service` systemd unit and removes public UFW rules for ports 80/443. All traffic routes through Cloudflare Edge to `http://127.0.0.1:80`.

### Tailscale SSH (Close Management Port 22)
```bash
# Preview changes first
izdeploy tunnel setup tailscale --key <TAILSCALE_KEY> --dry-run

# Execute provisioning on Linux host
izdeploy tunnel setup tailscale --key <TAILSCALE_KEY> --hostname iz-node-01
```
*Effect*: Enrolls VPS into the Tailnet, enables Tailscale SSH, and restricts port 22 strictly to the `tailscale0` virtual interface while denying all public SSH traffic.

### Verify Tunnel Status
```bash
izdeploy tunnel status
```

---

## 6. Application Volume Backups

### Command
```bash
izdeploy volume backup <volume_name>
```

### Actionable Rules
- Creates a timestamped `.tar.gz` archive of persistent container volume data.
- If offsite storage is configured, pass `--s3 s3://bucket/backups/`.

---

## 7. Operational Anti-Patterns (Strict Guardrails)

1. **NEVER call `izdeploy menu`**: The interactive wizard expects human keypresses. Use direct subcommands (`izdeploy status`, `izdeploy db list`, `izdeploy secret set`).
2. **NEVER manually edit `.agent/izdeploy.lock`**: The SHA-256 lockfile is cryptographically validated. Use `izdeploy deploy` to regenerate hashes or `--force` when intentional infrastructure modifications occur.
3. **NEVER open public ports 80/443 when Cloudflare Tunnel is active**: Modifying UFW to allow port 80/443 bypasses Cloudflare WAF protections.
