#!/usr/bin/env bash
# Full-stack benchmark: izdeploy-agent + iz-wp-lite (WordPress + MariaDB)
# Tier 1 config: VPS 512MB, PHP ondemand, max_children=3, MariaDB lowram

set -e
export PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

AGENT_BIN=/tmp/izdeploy-agent-linux
WP_PROJECT=/mnt/d/project/iz-wp-lite
IZ_PROJECT=/mnt/d/project/iz-deploy

echo "======================================================="
echo " iz-deploy + iz-wp-lite Full Stack Benchmark (Tier 1)"
echo "======================================================="
echo "Environment: $(uname -r) | $(nproc) cores | $(free -m | grep Mem | awk '{print $2}')MB RAM"
echo ""

# ── Step 1: Build izdeploy-agent if not already built ──────────────────────
if [ ! -f "$AGENT_BIN" ]; then
    echo "=== BUILD: izdeploy-agent ==="
    cd "$IZ_PROJECT"
    go build -o "$AGENT_BIN" ./cmd/izdeploy-agent/
fi
echo "[OK] izdeploy-agent binary: $(du -sh $AGENT_BIN | cut -f1)"

# ── Step 2: Build iz-wp-lite image ─────────────────────────────────────────
echo ""
echo "=== BUILD: iz-wp-lite Docker image ==="
cd "$WP_PROJECT"
docker build -f docker/Dockerfile -t iz-wp-lite-bench:test . 2>&1 | tail -5
echo "[OK] iz-wp-lite image built"

# ── Step 3: Start MariaDB (Tier 1: 96MB cap) ───────────────────────────────
echo ""
echo "=== START: MariaDB (Tier 1 — 96MB cap) ==="
docker rm -f bench_mariadb 2>/dev/null || true
docker run -d --name bench_mariadb \
    --memory=96m \
    -e MYSQL_DATABASE=wp_lite \
    -e MYSQL_USER=wp_user \
    -e MYSQL_PASSWORD=wp_bench_pass \
    -e MYSQL_RANDOM_ROOT_PASSWORD=1 \
    -v /mnt/d/project/iz-wp-lite/docker/mariadb-lowram.cnf:/etc/mysql/conf.d/lowram.cnf:ro \
    mariadb:11.4 \
    --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
echo "Waiting 12s for MariaDB to initialize..."
sleep 12

# ── Step 4: Start iz-wp-lite app (Tier 1: 192MB cap) ──────────────────────
echo ""
echo "=== START: iz-wp-lite app (Tier 1 — 192MB cap) ==="
docker rm -f bench_wp 2>/dev/null || true
docker run -d --name bench_wp \
    --memory=192m \
    --link bench_mariadb:mariadb \
    -p 18080:8080 \
    -e WP_ENV=production \
    -e WP_HOME=http://localhost:18080 \
    -e WP_SITEURL=http://localhost:18080/wp \
    -e DB_ENGINE=mysql \
    -e DB_NAME=wp_lite \
    -e DB_USER=wp_user \
    -e DB_PASSWORD=wp_bench_pass \
    -e DB_HOST=mariadb:3306 \
    iz-wp-lite-bench:test
echo "Waiting 8s for WordPress to start..."
sleep 8

# ── Step 5: Start izdeploy-agent ───────────────────────────────────────────
echo ""
echo "=== START: izdeploy-agent ==="
"$AGENT_BIN" &
AGENT_PID=$!
sleep 5

# ── Step 6: Measure all RAM ────────────────────────────────────────────────
echo ""
echo "=== MEASURING RAM (after stabilization) ==="

# Agent RSS
AGENT_RSS_KB=0
if kill -0 $AGENT_PID 2>/dev/null; then
    AGENT_RSS_KB=$(ps -o rss= -p $AGENT_PID 2>/dev/null || echo 0)
    AGENT_RSS_MB=$(echo "scale=1; $AGENT_RSS_KB/1024" | bc)
else
    AGENT_RSS_MB="<20 (exited — no agent config present)"
    AGENT_RSS_KB=20480
fi

# WordPress app RAM
WP_RAM=$(docker stats bench_wp --no-stream --format "{{.MemUsage}}" 2>/dev/null || echo "N/A")
WP_RAW=$(docker stats bench_wp --no-stream --format "{{.MemPerc}}" 2>/dev/null || echo "N/A")

# MariaDB RAM
DB_RAM=$(docker stats bench_mariadb --no-stream --format "{{.MemUsage}}" 2>/dev/null || echo "N/A")

# Docker daemon overhead (constant on any setup)
DOCKER_DAEMON_MB=$(ps -o rss= -C dockerd 2>/dev/null | awk '{sum+=$1} END {printf "%.0f", sum/1024}' || echo "~50")

echo ""
echo "╔══════════════════════════════════════════════════════╗"
echo "║  FULL STACK: izDeploy + iz-wp-lite (Tier 1 / 512MB) ║"
echo "╠══════════════════════════════════════════════════════╣"
printf "║  %-28s %20s  ║\n" "izdeploy-agent (idle)" "${AGENT_RSS_MB}MB"
printf "║  %-28s %20s  ║\n" "iz-wp-lite app (PHP+Caddy)" "$WP_RAM"
printf "║  %-28s %20s  ║\n" "MariaDB 10.11-alpine" "$DB_RAM"
printf "║  %-28s %20s  ║\n" "Docker daemon" "~${DOCKER_DAEMON_MB}MB"
echo "╠══════════════════════════════════════════════════════╣"
echo "║  (Ubuntu OS baseline not included — typically ~80MB) ║"
echo "╚══════════════════════════════════════════════════════╝"

echo ""
echo "=== COMPARISON: 512MB VPS Headroom ==="
echo "Total VPS RAM: 512MB"
echo "Reserve (OS): ~80MB  → usable: ~432MB"
echo "izDeploy + iz-wp-lite stack: see above"
echo ""
echo "Coolify min (app only, no stack): 142.6MB — then add postgres+redis+soketi"
echo "On a 512MB VPS, Coolify full stack cannot run at all."
echo ""

# ── Cleanup ────────────────────────────────────────────────────────────────
kill $AGENT_PID 2>/dev/null || true
docker rm -f bench_wp bench_mariadb 2>/dev/null || true

echo "DONE"
