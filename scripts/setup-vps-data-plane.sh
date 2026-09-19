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

echo "=== [3/5] Configuring Host Firewall (UFW) ==="

ufw default deny incoming
ufw default allow outgoing

# Port 80 & 443 for web traffic and proxy routing
ufw allow 80/tcp comment 'izdeploy HTTP traffic'
ufw allow 443/tcp comment 'izdeploy HTTPS traffic'

# SSH rule with optional rate limiting
if [ "${SSH_RATE_LIMIT}" = "true" ]; then
    ufw limit "${SSH_PORT}/tcp" comment 'izdeploy SSH rate-limited access'
else
    ufw allow "${SSH_PORT}/tcp" comment 'izdeploy SSH direct access'
fi

ufw --force enable
echo "Firewall active: incoming default deny, ports 80/443 open, SSH port ${SSH_PORT} secured."

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

echo "izDeploy data plane bootstrap complete. System ready for daemon installation."
