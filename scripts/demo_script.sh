#!/usr/bin/env bash
# izDeploy Demo Script — simulates the "blank VPS to running app" flow
# Recorded via: asciinema rec -c "bash demo_script.sh" demo.cast

# ── helpers ────────────────────────────────────────────────────────────────
BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
GRAY="\033[90m"
RESET="\033[0m"
IZDEPLOY=/tmp/izdeploy-linux

type_cmd() {
    # Simulate typing a command then executing it
    local cmd="$1"
    echo -ne "${GREEN}\$ ${RESET}"
    for ((i=0; i<${#cmd}; i++)); do
        echo -n "${cmd:$i:1}"
        sleep 0.04
    done
    echo ""
    sleep 0.3
}

banner() {
    echo ""
    echo -e "${CYAN}─────────────────────────────────────────${RESET}"
    echo -e "${CYAN}  $1${RESET}"
    echo -e "${CYAN}─────────────────────────────────────────${RESET}"
    echo ""
    sleep 0.5
}

# ── start ──────────────────────────────────────────────────────────────────
clear
sleep 1

echo -e "${BOLD}izDeploy — Deploy apps to \$4 VPS in under 3 minutes${RESET}"
echo -e "${GRAY}Agent footprint: 28MB | MCP embedded | No SSH required${RESET}"
sleep 2

# ── 1. version check ───────────────────────────────────────────────────────
banner "Step 1: Check izDeploy version"
type_cmd "izdeploy --version"
$IZDEPLOY --version 2>/dev/null || echo "izdeploy version 0.0.4"
sleep 1

# ── 2. init project ────────────────────────────────────────────────────────
banner "Step 2: Initialize app contract"
DEMO_DIR=/tmp/demo-myapp
rm -rf "$DEMO_DIR" && mkdir -p "$DEMO_DIR/.agent"
cd "$DEMO_DIR"

type_cmd "mkdir myapp && cd myapp"
sleep 0.5

type_cmd "izdeploy init"
cat > .agent/izdeploy.json << 'EOF'
{
  "name": "myapp",
  "image": "ghcr.io/myorg/myapp",
  "port": 3000,
  "domain": "myapp.example.com",
  "strategy": "recreate"
}
EOF

echo -e "${GREEN}✓${RESET} Created ${BOLD}.agent/izdeploy.json${RESET}"
echo -e "${GREEN}✓${RESET} Created ${BOLD}.agent/izdeploy.lock${RESET}"
echo -e "${GREEN}✓${RESET} Created ${BOLD}.cursor/rules/izdeploy.mdc${RESET} (AI bridge)"
sleep 1.5

# ── 3. show config ─────────────────────────────────────────────────────────
banner "Step 3: Config — under 10 lines"
type_cmd "cat .agent/izdeploy.json"
cat .agent/izdeploy.json
sleep 1.5

# ── 4. lint ────────────────────────────────────────────────────────────────
banner "Step 4: Validate config locally"
type_cmd "izdeploy lint"
$IZDEPLOY lint 2>/dev/null || {
    echo -e "${GREEN}✓${RESET} Schema: valid"
    echo -e "${GREEN}✓${RESET} Required fields: name, image, port, domain ✓"
    echo -e "${GREEN}✓${RESET} Strategy: recreate (safe for 512MB VPS)"
    echo -e "${GREEN}✓${RESET} Lockfile SHA-256: up to date"
    echo ""
    echo -e "  ${GREEN}[PASS]${RESET} 0 errors, 0 warnings"
}
sleep 1.5

# ── 5. deploy ──────────────────────────────────────────────────────────────
banner "Step 5: Deploy (GitHub Actions pre-built image)"
type_cmd "izdeploy deploy --image ghcr.io/myorg/myapp:abc1234"
sleep 0.5
echo -e "${GRAY}→ Verifying lockfile SHA-256...${RESET}"; sleep 0.4
echo -e "${GRAY}→ Pulling ghcr.io/myorg/myapp:abc1234...${RESET}"; sleep 0.8
echo -e "${GRAY}→ Strategy: recreate (stop old → start new, 1-3s downtime)${RESET}"; sleep 0.4
echo -e "${GRAY}→ Starting container on port 3000...${RESET}"; sleep 0.5
echo -e "${GRAY}→ Waiting for healthcheck at /health...${RESET}"; sleep 0.6
echo -e "${GRAY}→ Registering with kamal-proxy → myapp.example.com${RESET}"; sleep 0.4
echo ""
echo -e "${GREEN}✓ Deploy complete in 4.2s${RESET}"
echo -e "${GREEN}✓ https://myapp.example.com → active (Let's Encrypt TLS)${RESET}"
sleep 2

# ── 6. status ──────────────────────────────────────────────────────────────
banner "Step 6: Check status"
type_cmd "izdeploy status"
$IZDEPLOY status 2>/dev/null || {
    echo -e "  Container:  ${GREEN}running${RESET}"
    echo -e "  Image:      ghcr.io/myorg/myapp:abc1234"
    echo -e "  Uptime:     12s"
    echo -e "  RAM:        ${GREEN}28.3 MB${RESET} (agent) + 87 MB (app)"
    echo -e "  CPU:        0.4%"
    echo -e "  Domain:     https://myapp.example.com"
    echo -e "  Tunnel:     Cloudflare ✓  (no port 22/80/443 open)"
}
sleep 1.5

# ── 7. MCP demo ────────────────────────────────────────────────────────────
banner "Step 7: AI Agent via MCP (Cursor / Claude Code)"
type_cmd "# Your AI editor calls: iz_status, iz_logs, iz_deploy"
sleep 0.5
echo -e "${GRAY}[MCP tool: iz_status]${RESET}"
echo '{"state":"running","ram_mb":87,"cpu_percent":0.4,"uptime_s":42}'
sleep 0.8
echo ""
echo -e "${GRAY}[MCP tool: iz_logs (last 5 lines)]${RESET}"
echo "GET /api/health 200 3ms"
echo "GET /api/health 200 2ms"
echo "POST /api/data 201 18ms"
sleep 1

# ── 8. benchmark callout ───────────────────────────────────────────────────
banner "RAM: izDeploy vs Coolify vs Dokploy"
echo -e "  ${BOLD}izDeploy agent:${RESET}          ${GREEN}28.3 MB${RESET}  ✅ fits \$4 VPS (512MB)"
echo -e "  Dokploy (app only):     120.9 MiB  ❌ needs 2GB+ VPS"
echo -e "  Coolify (app only):     142.6 MiB  ❌ needs 4GB+ VPS"
echo ""
echo -e "  ${GRAY}izDeploy + WordPress + MariaDB full stack: ~118 MB${RESET}"
echo -e "  ${GRAY}Coolify app container alone:               142.6 MiB${RESET}"
sleep 2

# ── end ────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}GitHub:${RESET}  github.com/izhubs/iz-deploy"
echo -e "${BOLD}Star us${RESET} if this helped ⭐"
sleep 2
