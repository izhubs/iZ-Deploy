#!/usr/bin/env bash
# ==============================================================================
# iZ-Deploy Unified VPS Node Installer
# Usage: curl -sSL https://raw.githubusercontent.com/izhubs/iz-deploy/main/scripts/install-node.sh | sudo bash
# ==============================================================================

set -euo pipefail

REPO="izhubs/iZ-Deploy"
IZDEPLOY_USER="izdeploy"
IZDEPLOY_GROUP="izdeploy"
IZDEPLOY_HOME="/var/lib/izdeploy"
WEBHOOK_SECRET=$(openssl rand -hex 16)

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: Root privileges required. Execute with sudo or as root." >&2
    exit 1
fi

echo "=== [1/6] System Requirements Check ==="
if [ ! -f /etc/os-release ]; then
    echo "ERROR: Target host must be Ubuntu 24.04 LTS." >&2
    exit 1
fi
. /etc/os-release
if [ "${ID:-}" != "ubuntu" ] || [ "${VERSION_ID:-}" != "24.04" ]; then
    echo "WARNING: Officially only Ubuntu 24.04 LTS is supported. Continuing at your own risk." >&2
fi

# Detect RAM
TOTAL_RAM_MB=$(awk '/^MemTotal:/{print int($2/1024)}' /proc/meminfo)
echo "Detected RAM: ${TOTAL_RAM_MB}MB"

echo "=== [2/6] Running Data Plane Bootstrap (Docker, UFW, User) ==="
curl -sSL "https://raw.githubusercontent.com/${REPO}/main/scripts/setup-vps-data-plane.sh" | bash

if [ "${TOTAL_RAM_MB}" -lt 2048 ]; then
    echo "=== [3/6] RAM < 2GB. Running Memory Optimization (zRAM & Swap) ==="
    curl -sSL "https://raw.githubusercontent.com/${REPO}/main/scripts/setup-zram.sh" | bash
else
    echo "=== [3/6] RAM >= 2GB. Skipping zRAM Optimization ==="
fi

echo "=== [4/6] Downloading izDeploy Agent ==="
# Detect arch
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) TARGET_ARCH="amd64" ;;
  arm64|aarch64) TARGET_ARCH="arm64" ;;
  *) echo "ERROR: Unsupported architecture '${ARCH}'" >&2; exit 1 ;;
esac

# Get latest release tag
LATEST_TAG=$(curl -sSL -H "Accept: application/vnd.github.v3+json" "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')
if [ -z "${LATEST_TAG}" ]; then
    LATEST_TAG="v0.0.3" # Fallback
fi
CLEAN_VERSION="${LATEST_TAG#v}"
ARCHIVE_NAME="izdeploy_${CLEAN_VERSION}_linux_${TARGET_ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${ARCHIVE_NAME}"

echo "Downloading agent version ${LATEST_TAG}..."
TMP_DIR="$(mktemp -d)"
curl -sSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/${ARCHIVE_NAME}" || wget -qO "${TMP_DIR}/${ARCHIVE_NAME}" "${DOWNLOAD_URL}"
tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "${TMP_DIR}"

if [ -f "${TMP_DIR}/izdeploy-agent" ]; then
    mv "${TMP_DIR}/izdeploy-agent" /usr/local/bin/izdeploy-agent
    chmod +x /usr/local/bin/izdeploy-agent
fi
if [ -f "${TMP_DIR}/izdeploy-watchdog" ]; then
    mv "${TMP_DIR}/izdeploy-watchdog" /usr/local/bin/izdeploy-watchdog
    chmod +x /usr/local/bin/izdeploy-watchdog
fi
if [ -f "${TMP_DIR}/izdeploy" ]; then
    mv "${TMP_DIR}/izdeploy" /usr/local/bin/izdeploy
    chmod +x /usr/local/bin/izdeploy
fi
rm -rf "${TMP_DIR}"

echo "=== [5/6] Registering Systemd Services ==="
cat <<EOF > /etc/systemd/system/izdeploy-agent.service
[Unit]
Description=iZ-Deploy Agent Daemon
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/izdeploy-agent
Restart=always
RestartSec=5
Environment="IZDEPLOY_ENV=production"
Environment="IZDEPLOY_WEBHOOK_SECRET=${WEBHOOK_SECRET}"

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now izdeploy-agent.service

echo "=== [6/6] Installation Complete! ==="
if [ -n "${GH_USER:-}" ] && [ -n "${GH_PAT:-}" ]; then
    echo "Attempting automated GitHub Container Registry login..."
    echo "${GH_PAT}" | docker login ghcr.io -u "${GH_USER}" --password-stdin || echo "WARNING: GHCR login failed."
fi

VPS_IP=$(curl -s ifconfig.me)

echo "The iZ-Deploy agent is now running on port 8098."
echo "Check status with: systemctl status izdeploy-agent"
echo ""
echo "========================================================================="
echo "🔒 SECURITY CREDENTIALS (SAVE THIS!)"
echo "========================================================================="
echo "VPS IP Address: ${VPS_IP}"
echo "Webhook URL:    http://${VPS_IP}:8098/webhook"
echo "Webhook Secret: ${WEBHOOK_SECRET}"
echo ""
echo "========================================================================="
echo "🤖 AI AGENT HANDOFF: COPY AND PASTE THE LINK BELOW TO YOUR AI ASSISTANT"
echo "========================================================================="
echo "Link: https://raw.githubusercontent.com/izhubs/iZ-Deploy/main/AI_PROMPT.md"
echo ""
