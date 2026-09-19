#!/usr/bin/env sh
# ==============================================================================
# izDeploy CLI Installer
# Automated POSIX shell installation script for Linux and macOS
# ==============================================================================

set -eu

REPO="${REPO:-izhubs/iZ-Deploy}"
DEFAULT_FALLBACK_VERSION="0.1.0"
INSTALL_DIR="${INSTALL_DIR:-}"

# Detect host operating system
OS="$(uname -s)"
case "${OS}" in
  Linux)
    TARGET_OS="linux"
    ;;
  Darwin)
    TARGET_OS="darwin"
    ;;
  *)
    echo "Error: Operating system '${OS}' is unsupported. Only Linux and Darwin (macOS) are supported." >&2
    exit 1
    ;;
esac

# Detect host CPU architecture
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64)
    TARGET_ARCH="amd64"
    ;;
  arm64|aarch64)
    TARGET_ARCH="arm64"
    ;;
  *)
    echo "Error: CPU architecture '${ARCH}' is unsupported. Supported: x86_64/amd64, arm64/aarch64." >&2
    exit 1
    ;;
esac

# Resolve release version
if [ -z "${VERSION:-}" ]; then
  API_URL="https://api.github.com/repos/${REPO}/releases/latest"
  LATEST_TAG=""

  if command -v curl >/dev/null 2>&1; then
    LATEST_TAG="$(curl -fsSL -H "Accept: application/vnd.github.v3+json" "${API_URL}" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"tag_name": "([^"]+)".*/\1/' || true)"
  elif command -v wget >/dev/null 2>&1; then
    LATEST_TAG="$(wget -qO- --header="Accept: application/vnd.github.v3+json" "${API_URL}" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"tag_name": "([^"]+)".*/\1/' || true)"
  fi

  if [ -n "${LATEST_TAG}" ]; then
    VERSION="${LATEST_TAG}"
  else
    VERSION="${DEFAULT_FALLBACK_VERSION}"
    echo "Notice: GitHub API rate limited or unreachable. Using fallback version ${VERSION}." >&2
  fi
fi

# Normalize version formatting
CLEAN_VERSION="${VERSION#v}"
TAG="v${CLEAN_VERSION}"

ARCHIVE_NAME="izdeploy_${CLEAN_VERSION}_${TARGET_OS}_${TARGET_ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE_NAME}"

# Determine target directory
if [ -z "${INSTALL_DIR}" ]; then
  if [ -w "/usr/local/bin" ] || [ "$(id -u 2>/dev/null || true)" = "0" ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
  fi
fi

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'izdeploy')"
cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT INT TERM

echo "Downloading ${ARCHIVE_NAME} from ${DOWNLOAD_URL}..."

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/${ARCHIVE_NAME}"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "${TMP_DIR}/${ARCHIVE_NAME}" "${DOWNLOAD_URL}"
else
  echo "Error: Neither curl nor wget was found. Install curl or wget to continue." >&2
  exit 1
fi

tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "${TMP_DIR}"

if [ ! -f "${TMP_DIR}/izdeploy" ]; then
  echo "Error: Binary 'izdeploy' not found in downloaded archive." >&2
  exit 1
fi

mkdir -p "${INSTALL_DIR}"
mv "${TMP_DIR}/izdeploy" "${INSTALL_DIR}/izdeploy"
chmod +x "${INSTALL_DIR}/izdeploy"

echo "izDeploy installed successfully to ${INSTALL_DIR}/izdeploy"

if command -v izdeploy >/dev/null 2>&1; then
  izdeploy --version
else
  "${INSTALL_DIR}/izdeploy" --version || true
  case ":${PATH}:" in
    *:"${INSTALL_DIR}":*) ;;
    *)
      echo "Notice: '${INSTALL_DIR}' is not in your PATH."
      echo "Add it with: export PATH=\"${INSTALL_DIR}:\$PATH\""
      ;;
  esac
fi
