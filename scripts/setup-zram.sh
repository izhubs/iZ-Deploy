#!/usr/bin/env bash
# ==============================================================================
# izDeploy zRAM & Kernel Memory Optimization Script
# Target: Ubuntu 24.04 LTS (x86_64 / aarch64)
# Configures: 512MB zRAM (LZ4, priority 100), 1GB disk swap fallback (priority 10),
#             sysctl memory settings, and validates cgroups v2.
# Idempotent execution: safe to rerun multiple times
# ==============================================================================

set -euo pipefail

ZRAM_SIZE="512M"
ZRAM_ALGO="lz4"
ZRAM_PRIORITY=100
SWAP_FILE="/swapfile_fallback"
SWAP_SIZE="2G"
SWAP_PRIORITY=10

echo "=== [1/4] Verifying cgroups v2 Architecture ==="

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: Root privileges required. Execute with sudo or as root." >&2
    exit 1
fi

CGROUP_FS="$(stat -fc %T /sys/fs/cgroup/ 2>/dev/null || true)"
if [ "${CGROUP_FS}" != "cgroup2fs" ]; then
    echo "WARNING: /sys/fs/cgroup filesystem is '${CGROUP_FS}', expected 'cgroup2fs'." >&2
    echo "Verify kernel boot parameters include 'systemd.unified_cgroup_hierarchy=1'." >&2
else
    echo "cgroups v2 hierarchy (cgroup2fs) confirmed active."
fi

echo "=== [2/4] Applying Kernel Memory Parameters (sysctl) ==="

SYSCTL_CONF="/etc/sysctl.d/99-izdeploy-memory.conf"
cat <<'EOF' > "${SYSCTL_CONF}"
# izDeploy memory tuning for low-footprint VPS instances
vm.swappiness = 5
vm.vfs_cache_pressure = 50
EOF

sysctl -p "${SYSCTL_CONF}" >/dev/null
echo "sysctl: vm.swappiness=$(sysctl -n vm.swappiness), vm.vfs_cache_pressure=$(sysctl -n vm.vfs_cache_pressure)"

echo "=== [3/4] Provisioning 1GB Fallback Disk Swap (Priority ${SWAP_PRIORITY}) ==="

if [ ! -f "${SWAP_FILE}" ]; then
    echo "Allocating ${SWAP_SIZE} swapfile at ${SWAP_FILE}..."
    fallocate -l "${SWAP_SIZE}" "${SWAP_FILE}" || dd if=/dev/zero of="${SWAP_FILE}" bs=1M count=1024 status=none
    chmod 600 "${SWAP_FILE}"
    mkswap "${SWAP_FILE}" >/dev/null
fi

if ! grep -qF "${SWAP_FILE}" /proc/swaps; then
    swapon -p "${SWAP_PRIORITY}" "${SWAP_FILE}"
    echo "Fallback swap activated at ${SWAP_FILE}."
else
    echo "Fallback swap already active at ${SWAP_FILE}."
fi

FSTAB_ENTRY="${SWAP_FILE} none swap sw,pri=${SWAP_PRIORITY} 0 0"
if ! grep -qF "${SWAP_FILE}" /etc/fstab; then
    echo "${FSTAB_ENTRY}" >> /etc/fstab
fi

echo "=== [4/4] Configuring 512MB zRAM Device (LZ4, Priority ${ZRAM_PRIORITY}) ==="

# Ensure zram kernel module loads on boot
MODULES_CONF="/etc/modules-load.d/izdeploy-zram.conf"
if [ ! -f "${MODULES_CONF}" ] || ! grep -qF "zram" "${MODULES_CONF}"; then
    echo "zram" > "${MODULES_CONF}"
fi

modprobe zram num_devices=1 2>/dev/null || true

# Provision persistent systemd service for zram lifecycle
ZRAM_SERVICE="/etc/systemd/system/izdeploy-zram.service"
cat <<EOF > "${ZRAM_SERVICE}"
[Unit]
Description=izDeploy zRAM Swap Device Setup
After=local-fs.target

[Service]
Type=oneshot
RemainAfterExit=true
ExecStart=/usr/local/sbin/izdeploy-zram-start.sh
ExecStop=/usr/local/sbin/izdeploy-zram-stop.sh

[Install]
WantedBy=multi-user.target
EOF

cat <<EOF > /usr/local/sbin/izdeploy-zram-start.sh
#!/usr/bin/env bash
set -euo pipefail

modprobe zram num_devices=1 2>/dev/null || true

if ! grep -qF "/dev/zram0" /proc/swaps; then
    if [ -f /sys/block/zram0/reset ]; then
        echo 1 > /sys/block/zram0/reset 2>/dev/null || true
    fi
    if grep -qF "${ZRAM_ALGO}" /sys/block/zram0/comp_algorithm; then
        echo "${ZRAM_ALGO}" > /sys/block/zram0/comp_algorithm
    fi
    echo "${ZRAM_SIZE}" > /sys/block/zram0/disksize
    mkswap -L zram0 /dev/zram0 >/dev/null
    swapon -p ${ZRAM_PRIORITY} /dev/zram0
fi
EOF

cat <<'EOF' > /usr/local/sbin/izdeploy-zram-stop.sh
#!/usr/bin/env bash
set -euo pipefail

if grep -qF "/dev/zram0" /proc/swaps; then
    swapoff /dev/zram0 2>/dev/null || true
fi
if [ -f /sys/block/zram0/reset ]; then
    echo 1 > /sys/block/zram0/reset 2>/dev/null || true
fi
EOF

chmod 755 /usr/local/sbin/izdeploy-zram-start.sh /usr/local/sbin/izdeploy-zram-stop.sh

# Run immediately
bash /usr/local/sbin/izdeploy-zram-start.sh

systemctl daemon-reload
systemctl enable izdeploy-zram.service

echo "zRAM configured and active:"
swapon --show || cat /proc/swaps
echo "Memory optimization completed successfully."
