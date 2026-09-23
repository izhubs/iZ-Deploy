#!/usr/bin/env bash
# ==============================================================================
# izDeploy Data Plane Bootstrap Script
# Target: Ubuntu 24.04 LTS (x86_64 / aarch64)
# Idempotent execution: safe to rerun multiple times
# ==============================================================================

set -euo pipefail

# Configuration defaults
SSH_PORT="${SSH_PORT:-22}"
SSH_RATE_LIMIT="${SSH_RATE_LIMIT:-true}"
IZDEPLOY_USER="izdeploy"
IZDEPLOY_GROUP="izdeploy"
IZDEPLOY_HOME="/var/lib/izdeploy"
TAILSCALE_KEY="${TAILSCALE_KEY:-}"
CF_TUNNEL_TOKEN="${CF_TUNNEL_TOKEN:-}"
NODE_NAME="${NODE_NAME:-}"
ENABLE_FAIL2BAN="${ENABLE_FAIL2BAN:-true}"
ENABLE_UNATTENDED_UPGRADES="${ENABLE_UNATTENDED_UPGRADES:-true}"

echo "=== [1/5] Validating Host Architecture and OS Distribution ==="

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: Root privileges required. Execute with sudo or as root." >&2
    exit 1
fi

if [ -f /etc/os-release ]; then
    # shellcheck source=/dev/null
    . /etc/os-release
else
    echo "ERROR: /etc/os-release missing. Target host must be Ubuntu 24.04 LTS." >&2
    exit 1
fi

if [ "${ID:-}" != "ubuntu" ] || [ "${VERSION_ID:-}" != "24.04" ]; then
    echo "ERROR: Unsupported operating system '${ID:-unknown} ${VERSION_ID:-unknown}'. Only Ubuntu 24.04 LTS is supported." >&2
    exit 1
fi

SYSTEM_ARCH="$(uname -m)"
case "${SYSTEM_ARCH}" in
    x86_64)
        DPKG_ARCH="amd64"
        ;;
    aarch64|arm64)
        DPKG_ARCH="arm64"
        ;;
    *)
        echo "ERROR: Unsupported architecture '${SYSTEM_ARCH}'. Supported: x86_64, aarch64." >&2
        exit 1
        ;;
esac

echo "OS: Ubuntu 24.04 LTS (${SYSTEM_ARCH} / ${DPKG_ARCH}) verified."

echo "=== [2/5] Installing Docker Engine & containerd from Official Apt Repo ==="

export DEBIAN_FRONTEND=noninteractive

apt-get update -y
apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg \
    iptables \
    ufw

install -m 0755 -d /etc/apt/keyrings

if [ ! -f /etc/apt/keyrings/docker.asc ]; then
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
fi

DOCKER_LIST="/etc/apt/sources.list.d/docker.list"
DOCKER_SOURCE="deb [arch=${DPKG_ARCH} signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable"

if [ ! -f "${DOCKER_LIST}" ] || ! grep -qF "${DOCKER_SOURCE}" "${DOCKER_LIST}"; then
    echo "${DOCKER_SOURCE}" > "${DOCKER_LIST}"
    apt-get update -y
fi

apt-get install -y --no-install-recommends \
    docker-ce \
    docker-ce-cli \
    containerd.io \
    docker-buildx-plugin \
    docker-compose-plugin

# Configure Docker daemon log rotation defaults if not present
DOCKER_CONFIG_DIR="/etc/docker"
DOCKER_DAEMON_JSON="${DOCKER_CONFIG_DIR}/daemon.json"
mkdir -p "${DOCKER_CONFIG_DIR}"

if [ ! -f "${DOCKER_DAEMON_JSON}" ]; then
    cat <<'EOF' > "${DOCKER_DAEMON_JSON}"
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
EOF
    systemctl restart docker || true
fi

systemctl enable docker
systemctl start docker

echo "Docker Engine $(docker --version) active."

echo "=== [3/5] Configuring Host Firewall (UFW) & Secure Networking ==="

# Optional Tailscale Setup
if [ -n "${TAILSCALE_KEY}" ]; then
    echo "Provisioning Tailscale Mesh and Tailscale SSH..."
    if ! command -v tailscale >/dev/null 2>&1; then
        curl -fsSL https://tailscale.com/install.sh | sh
    fi
    TS_HOSTNAME="${NODE_NAME:-iz-node-$(hostname)}"
    tailscale up --authkey="${TAILSCALE_KEY}" --ssh --hostname="${TS_HOSTNAME}" --reset || true
    echo "Tailscale active with hostname '${TS_HOSTNAME}' and Tailscale SSH."
fi

# Optional Cloudflare Tunnel Setup
if [ -n "${CF_TUNNEL_TOKEN}" ]; then
    echo "Provisioning Cloudflare Tunnel (cloudflared)..."
    if ! command -v cloudflared >/dev/null 2>&1; then
        CF_DEB_URL="https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${DPKG_ARCH}.deb"
        curl -sSL "${CF_DEB_URL}" -o /tmp/cloudflared.deb
        dpkg -i /tmp/cloudflared.deb || apt-get install -f -y
        rm -f /tmp/cloudflared.deb
    fi
    cloudflared service install "${CF_TUNNEL_TOKEN}" || true
    systemctl enable cloudflared || true
    systemctl restart cloudflared || true
    echo "Cloudflare Tunnel active."
fi

ufw default deny incoming
ufw default allow outgoing

# Port 80 & 443 web traffic rules
if [ -n "${CF_TUNNEL_TOKEN}" ]; then
    echo "Cloudflare Tunnel active: keeping incoming public ports 80/443 closed."
else
    ufw allow 80/tcp comment 'izdeploy HTTP traffic'
    ufw allow 443/tcp comment 'izdeploy HTTPS traffic'
fi

# SSH rule: Tailscale only vs Public SSH
if [ -n "${TAILSCALE_KEY}" ]; then
    ufw allow in on tailscale0 to any port "${SSH_PORT}" proto tcp comment 'izdeploy SSH via Tailscale only'
    echo "Public port ${SSH_PORT} closed. SSH restricted strictly to Tailscale network."
else
    if [ "${SSH_RATE_LIMIT}" = "true" ]; then
        ufw limit "${SSH_PORT}/tcp" comment 'izdeploy SSH rate-limited access'
    else
        ufw allow "${SSH_PORT}/tcp" comment 'izdeploy SSH direct access'
    fi
fi

ufw --force enable
echo "Firewall active: incoming default deny configured."

# Fail2ban when public SSH is open
if [ -z "${TAILSCALE_KEY}" ] && [ "${ENABLE_FAIL2BAN}" = "true" ]; then
    echo "Public SSH detected: installing fail2ban for automated IP protection..."
    apt-get install -y --no-install-recommends fail2ban
    systemctl enable fail2ban || true
    systemctl restart fail2ban || true
fi

# Unattended security upgrades
if [ "${ENABLE_UNATTENDED_UPGRADES}" = "true" ]; then
    echo "Configuring automatic security updates..."
    apt-get install -y --no-install-recommends unattended-upgrades
    mkdir -p /etc/apt/apt.conf.d
    echo 'APT::Periodic::Update-Package-Lists "1";' > /etc/apt/apt.conf.d/20auto-upgrades
    echo 'APT::Periodic::Unattended-Upgrade "1";' >> /etc/apt/apt.conf.d/20auto-upgrades
    systemctl restart unattended-upgrades || true
fi

echo "=== [4/5] Provisioning System User & Group Permissions ==="

if ! getent group "${IZDEPLOY_GROUP}" >/dev/null 2>&1; then
    groupadd --system "${IZDEPLOY_GROUP}"
fi

if ! getent group docker >/dev/null 2>&1; then
    groupadd --system docker
fi

if ! id -u "${IZDEPLOY_USER}" >/dev/null 2>&1; then
    useradd --system \
        --gid "${IZDEPLOY_GROUP}" \
        --groups docker \
        --home-dir "${IZDEPLOY_HOME}" \
        --shell /usr/sbin/nologin \
        --comment "izDeploy runtime daemon" \
        "${IZDEPLOY_USER}"
else
    usermod -aG docker "${IZDEPLOY_USER}"
fi

echo "=== [5/5] Creating izDeploy Working Directory Structure ==="

for DIR in "/etc/izdeploy" "${IZDEPLOY_HOME}" "/var/log/izdeploy"; do
    mkdir -p "${DIR}"
    chown -R "${IZDEPLOY_USER}:${IZDEPLOY_GROUP}" "${DIR}"
    chmod 750 "${DIR}"
done

echo "=== [6/6] Configuring zRAM and Memory Limits ==="
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "${SCRIPT_DIR}/setup-zram.sh" ]; then
    bash "${SCRIPT_DIR}/setup-zram.sh"
else
    echo "Warning: setup-zram.sh not found."
fi

echo "=== [7/7] Setting up Automated Docker GC ==="
CRON_FILE="/etc/cron.weekly/izdeploy-docker-gc"
cat <<'EOF' > "${CRON_FILE}"
#!/usr/bin/env bash
docker system prune -af --filter "until=168h" >/var/log/izdeploy/docker-gc.log 2>&1
EOF
chmod +x "${CRON_FILE}"
echo "Docker GC cronjob installed at ${CRON_FILE}."

echo "izDeploy data plane bootstrap complete. System ready for daemon installation."
