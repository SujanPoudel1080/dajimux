#!/usr/bin/env bash
# ==============================================================================
# dajimux installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/SujanPoudel1080/dajimux/main/install.sh | bash
# Or with a specific version:
#   curl -fsSL https://raw.githubusercontent.com/SujanPoudel1080/dajimux/main/install.sh | VERSION=v0.1.0 bash
# ==============================================================================

set -euo pipefail

REPO="SujanPoudel1080/dajimux"
BINARY_NAME="dajimux"

# Color formatting
BOLD="\033[1m"
GREEN="\033[0;32m"
CYAN="\033[0;36m"
YELLOW="\033[1;33m"
RED="\033[0;31m"
RESET="\033[0m"

log_info() {
    printf "${CYAN}[INFO]${RESET} %s\n" "$1"
}

log_success() {
    printf "${GREEN}[SUCCESS]${RESET} %s\n" "$1"
}

log_warn() {
    printf "${YELLOW}[WARN]${RESET} %s\n" "$1"
}

log_error() {
    printf "${RED}[ERROR]${RESET} %s\n" "$1" >&2
}

# 1. Detect Operating System
detect_os() {
    local os
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$os" in
        darwin) echo "darwin" ;;
        linux)  echo "linux" ;;
        *)
            log_error "Unsupported operating system: $os. dajimux currently supports macOS (darwin) and Linux."
            exit 1
            ;;
    esac
}

# 2. Detect CPU Architecture
detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)   echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)
            log_error "Unsupported CPU architecture: $arch. dajimux provides precompiled binaries for amd64 and arm64."
            exit 1
            ;;
    esac
}

# 3. Resolve Target Version
get_latest_version() {
    local latest
    if command -v curl >/dev/null 2>&1; then
        latest=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    elif command -v wget >/dev/null 2>&1; then
        latest=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    fi

    if [ -z "${latest:-}" ]; then
        latest="v0.1.0"
    fi
    echo "$latest"
}

main() {
    printf "\n"
    printf "${BOLD}${CYAN}  ██████╗  █████╗      ██╗██╗███╗   ███╗██╗   ██╗██╗  ██╗${RESET}\n"
    printf "${BOLD}${CYAN}  ██╔══██╗██╔══██╗     ██║██║████╗ ████║██║   ██║╚██╗██╔╝${RESET}\n"
    printf "${BOLD}${CYAN}  ██║  ██║███████║     ██║██║██╔████╔██║██║   ██║ ╚███╔╝ ${RESET}\n"
    printf "${BOLD}${CYAN}  ██║  ██║██╔══██║██   ██║██║██║╚██╔╝██║██║   ██║ ██╔██╗ ${RESET}\n"
    printf "${BOLD}${CYAN}  ██████╔╝██║  ██║╚█████╔╝██║██║ ╚═╝ ██║╚██████╔╝██╔╝ ██╗${RESET}\n"
    printf "${BOLD}${CYAN}  ╚═════╝ ╚═╝  ╚═╝ ╚════╝ ╚═╝╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═╝${RESET}\n"
    printf "\n"
    log_info "Installer starting..."

    OS="$(detect_os)"
    ARCH="$(detect_arch)"
    log_info "Detected Platform: ${OS}/${ARCH}"

    TAG="${VERSION:-$(get_latest_version)}"
    VERSION_NUM="${TAG#v}"
    log_info "Target Version: ${TAG} (${VERSION_NUM})"

    TAR_NAME="${BINARY_NAME}_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${TAR_NAME}"
    CHECKSUM_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"

    TMP_DIR="$(mktemp -d -t dajimux-install-XXXXXX)"
    trap 'rm -rf "${TMP_DIR}"' EXIT

    log_info "Downloading ${DOWNLOAD_URL}..."
    if command -v curl >/dev/null 2>&1; then
        curl -fSL "$DOWNLOAD_URL" -o "${TMP_DIR}/${TAR_NAME}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${TMP_DIR}/${TAR_NAME}" "$DOWNLOAD_URL"
    else
        log_error "Neither curl nor wget found. Please install curl or wget."
        exit 1
    fi

    # Verify checksum if checksums.txt is downloadable
    log_info "Verifying SHA256 checksum..."
    CHECKSUM_FILE="${TMP_DIR}/checksums.txt"
    if curl -fsSL "$CHECKSUM_URL" -o "$CHECKSUM_FILE" 2>/dev/null; then
        (
            cd "$TMP_DIR"
            if command -v sha256sum >/dev/null 2>&1; then
                grep "${TAR_NAME}" checksums.txt | sha256sum --check --status 2>/dev/null || log_warn "Checksum verification skipped or failed."
            elif command -v shasum >/dev/null 2>&1; then
                grep "${TAR_NAME}" checksums.txt | shasum -a 256 --check --status 2>/dev/null || log_warn "Checksum verification skipped or failed."
            fi
        )
        log_success "Checksum verified."
    else
        log_warn "Checksums file not found or release not finalized. Proceeding with caution."
    fi

    log_info "Extracting ${TAR_NAME}..."
    tar -xzf "${TMP_DIR}/${TAR_NAME}" -C "${TMP_DIR}"

    if [ ! -f "${TMP_DIR}/${BINARY_NAME}" ]; then
        log_error "Binary '${BINARY_NAME}' not found inside archive."
        exit 1
    fi
    chmod +x "${TMP_DIR}/${BINARY_NAME}"

    # Determine installation directory
    INSTALL_DIR="${INSTALL_DIR:-}"
    if [ -z "$INSTALL_DIR" ]; then
        if [ -w "/usr/local/bin" ]; then
            INSTALL_DIR="/usr/local/bin"
            USE_SUDO=0
        elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
            INSTALL_DIR="/usr/local/bin"
            USE_SUDO=1
        else
            INSTALL_DIR="${HOME}/.local/bin"
            USE_SUDO=0
            mkdir -p "$INSTALL_DIR"
        fi
    else
        USE_SUDO=0
    fi

    TARGET_PATH="${INSTALL_DIR}/${BINARY_NAME}"
    log_info "Installing binary to ${TARGET_PATH}..."

    if [ "${USE_SUDO:-0}" -eq 1 ]; then
        sudo cp "${TMP_DIR}/${BINARY_NAME}" "${TARGET_PATH}"
        sudo chmod 755 "${TARGET_PATH}"
    else
        cp "${TMP_DIR}/${BINARY_NAME}" "${TARGET_PATH}"
        chmod 755 "${TARGET_PATH}"
    fi

    # Check PATH
    if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
        log_warn "Notice: ${INSTALL_DIR} is not in your PATH."
        log_warn "Add it to your ~/.bashrc or ~/.zshrc:"
        log_warn "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi

    log_success "dajimux successfully installed!"
    if command -v "${TARGET_PATH}" >/dev/null 2>&1; then
        INSTALLED_VER="$("${TARGET_PATH}" version 2>/dev/null || true)"
        log_info "Installed: ${INSTALLED_VER}"
    fi

    printf "\n"
    printf "${BOLD}${GREEN}Get Started:${RESET}\n"
    printf "  • Launch interactive multiplexer:    ${BOLD}dajimux${RESET}\n"
    printf "  • List active sessions:             ${BOLD}dajimux list${RESET}\n"
    printf "  • Trigger command mode:             ${BOLD}Press Space twice (<Space><Space>)${RESET}\n"
    printf "  • Split horizontally / vertically:  ${BOLD}Space Space s  /  Space Space v${RESET}\n"
    printf "  • Detach session:                   ${BOLD}Space Space d${RESET}\n"
    printf "  • View documentation:               ${BOLD}https://github.com/${REPO}${RESET}\n"
    printf "\n"
}

main "$@"
