#!/usr/bin/env bash
# ==============================================================================
# TermChat Installer Script
# Detects operating system and architecture, downloads the appropriate release
# binary from GitHub, installs it, and configures the user's PATH.
# ==============================================================================

set -e

# Default settings
DEFAULT_REPO="ankur3-101106/termchat-v2"
REPO="${TERMCHAT_REPO:-$DEFAULT_REPO}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-}"
NO_MODIFY_PATH="${NO_MODIFY_PATH:-0}"

# Color codes (only when outputting to an interactive terminal)
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BOLD="$(printf '\033[1m')"
  GREEN="$(printf '\033[0;32m')"
  BLUE="$(printf '\033[0;34m')"
  CYAN="$(printf '\033[0;36m')"
  YELLOW="$(printf '\033[1;33m')"
  RED="$(printf '\033[0;31m')"
  RESET="$(printf '\033[0m')"
else
  BOLD=""
  GREEN=""
  BLUE=""
  CYAN=""
  YELLOW=""
  RED=""
  RESET=""
fi

log_info() {
  printf "%sℹ%s %s\n" "$BLUE" "$RESET" "$1"
}

log_step() {
  printf "%s==>%s %s%s%s\n" "$CYAN" "$RESET" "$BOLD" "$1" "$RESET"
}

log_success() {
  printf "%s✔%s %s\n" "$GREEN" "$RESET" "$1"
}

log_warn() {
  printf "%s⚠%s %s\n" "$YELLOW" "$RESET" "$1"
}

log_error() {
  printf "%s✖ Error:%s %s\n" "$RED" "$RESET" "$1" >&2
}

show_help() {
  cat << EOF
TermChat Installer

Usage:
  install.sh [options]
  curl -fsSL https://raw.githubusercontent.com/$REPO/master/install.sh | bash -s -- [options]

Options:
  -d, --dir <path>       Install destination directory
                         (default: ~/.termchat/bin or \$PREFIX/bin on Termux)
  -v, --version <tag>    Specific release version to install (default: latest)
  --no-modify-path       Do not attempt to add install directory to shell PATH
  -h, --help             Show this help message

Environment Variables:
  INSTALL_DIR            Same as --dir
  VERSION                Same as --version
  TERMCHAT_REPO          GitHub repository (default: $DEFAULT_REPO)
  NO_MODIFY_PATH         Set to 1 to skip modifying shell configuration files
EOF
}

# Parse command line flags
while [ "$#" -gt 0 ]; do
  case "$1" in
    -d|--dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    -v|--version)
      VERSION="$2"
      shift 2
      ;;
    --no-modify-path)
      NO_MODIFY_PATH=1
      shift
      ;;
    -h|--help)
      show_help
      exit 0
      ;;
    *)
      log_error "Unknown option: $1"
      show_help
      exit 1
      ;;
  esac
done

# Banner
printf "%s💬 TermChat Installer%s\n" "$BOLD" "$RESET"
echo "======================================"

# 1. Detect Operating System
log_step "Detecting operating system..."
RAW_OS="$(uname -s)"
OS_LOWER="$(echo "$RAW_OS" | tr '[:upper:]' '[:lower:]')"

IS_TERMUX=0
if [ -n "${TERMUX_VERSION:-}" ] || [ -d "/data/data/com.termux" ]; then
  IS_TERMUX=1
fi

case "$OS_LOWER" in
  linux*)
    if [ "$IS_TERMUX" -eq 1 ]; then
      OS_NAME="termux"
      OS_DISPLAY="Android (Termux)"
    else
      OS_NAME="linux"
      OS_DISPLAY="Linux"
    fi
    EXE_EXT=""
    ;;
  darwin*)
    OS_NAME="macos"
    OS_DISPLAY="macOS"
    EXE_EXT=""
    ;;
  cygwin*|mingw*|msys*)
    OS_NAME="windows"
    OS_DISPLAY="Windows"
    EXE_EXT=".exe"
    ;;
  *)
    log_error "Unsupported operating system: $RAW_OS"
    exit 1
    ;;
esac

log_info "Detected OS: ${BOLD}$OS_DISPLAY${RESET}"

# 2. Detect Hardware Architecture
log_step "Detecting architecture..."
RAW_ARCH="$(uname -m)"
ARCH_LOWER="$(echo "$RAW_ARCH" | tr '[:upper:]' '[:lower:]')"

case "$ARCH_LOWER" in
  x86_64|amd64)
    ARCH_NAME="amd64"
    ARCH_DISPLAY="x86_64 (amd64)"
    ;;
  aarch64|arm64)
    ARCH_NAME="arm64"
    ARCH_DISPLAY="ARM64 (aarch64)"
    ;;
  *)
    log_error "Unsupported CPU architecture: $RAW_ARCH"
    echo "Pre-built binaries are available for x86_64 (amd64) and ARM64 (aarch64)." >&2
    echo "To compile TermChat for your architecture, run: go build ./cmd/client" >&2
    exit 1
    ;;
esac

log_info "Detected Architecture: ${BOLD}$ARCH_DISPLAY${RESET}"

# 3. Match release binary asset name
if [ "$OS_NAME" = "termux" ] && [ "$ARCH_NAME" = "arm64" ]; then
  ASSET_NAME="termchat-termux-arm64"
elif [ "$OS_NAME" = "windows" ]; then
  ASSET_NAME="termchat-windows-${ARCH_NAME}.exe"
else
  ASSET_NAME="termchat-${OS_NAME}-${ARCH_NAME}"
fi

TARGET_BIN_NAME="termchat${EXE_EXT}"
log_info "Matched release binary: ${BOLD}$ASSET_NAME${RESET}"

# 4. Resolve download URL
if [ "$VERSION" = "latest" ]; then
  DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
else
  case "$VERSION" in
    v*) TAG="$VERSION" ;;
    *) TAG="v$VERSION" ;;
  esac
  DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET_NAME}"
fi

# Detect download tool (curl or wget)
if command -v curl >/dev/null 2>&1; then
  DOWNLOAD_TOOL="curl"
elif command -v wget >/dev/null 2>&1; then
  DOWNLOAD_TOOL="wget"
else
  log_error "Neither 'curl' nor 'wget' was found. Please install one of them to proceed."
  exit 1
fi

# Attempt to extract resolved version tag if 'latest'
DISPLAY_VERSION="$VERSION"
if [ "$VERSION" = "latest" ] && [ "$DOWNLOAD_TOOL" = "curl" ]; then
  RESOLVED_TAG="$(curl -sIL "$DOWNLOAD_URL" 2>/dev/null | grep -i '^location:' | sed -n 's/.*\/releases\/download\/\([^\/]*\)\/.*/\1/p' | head -n 1 | tr -d '\r\n')"
  if [ -n "$RESOLVED_TAG" ]; then
    DISPLAY_VERSION="$RESOLVED_TAG"
  fi
fi

# 5. Determine install directory
if [ -z "$INSTALL_DIR" ]; then
  if [ "$IS_TERMUX" -eq 1 ] && [ -n "${PREFIX:-}" ] && [ -d "$PREFIX/bin" ] && [ -w "$PREFIX/bin" ]; then
    INSTALL_DIR="$PREFIX/bin"
  elif [ "$(id -u 2>/dev/null || echo 1)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="$HOME/.termchat/bin"
  fi
fi

log_step "Installing TermChat ${DISPLAY_VERSION}..."
log_info "Install directory: ${BOLD}$INSTALL_DIR${RESET}"
log_info "Downloading from: ${DOWNLOAD_URL}"

# Create temporary staging directory
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'termchat-install.XXXXXX')"
trap 'rm -rf "$TMP_DIR"' EXIT

DOWNLOAD_FILE="$TMP_DIR/$ASSET_NAME"

# Download the binary
if [ "$DOWNLOAD_TOOL" = "curl" ]; then
  if [ -t 1 ]; then
    CURL_PROGRESS="--progress-bar"
  else
    CURL_PROGRESS="-s"
  fi
  if ! curl -fSL $CURL_PROGRESS "$DOWNLOAD_URL" -o "$DOWNLOAD_FILE"; then
    log_error "Failed to download $DOWNLOAD_URL"
    exit 1
  fi
elif [ "$DOWNLOAD_TOOL" = "wget" ]; then
  if [ -t 1 ]; then
    WGET_PROGRESS="--show-progress -q"
  else
    WGET_PROGRESS="-q"
  fi
  if ! wget $WGET_PROGRESS -O "$DOWNLOAD_FILE" "$DOWNLOAD_URL"; then
    log_error "Failed to download $DOWNLOAD_URL"
    exit 1
  fi
fi

# Ensure executable permissions
chmod +x "$DOWNLOAD_FILE"

# Verify binary integrity (ensure not empty)
if [ ! -s "$DOWNLOAD_FILE" ]; then
  log_error "Downloaded binary is empty."
  exit 1
fi

# Ensure destination directory exists
mkdir -p "$INSTALL_DIR"

# Move binary to target destination
DEST_PATH="$INSTALL_DIR/$TARGET_BIN_NAME"
mv -f "$DOWNLOAD_FILE" "$DEST_PATH"
chmod +x "$DEST_PATH"

log_success "Binary installed to ${BOLD}$DEST_PATH${RESET}"

# 6. Configure environment PATH
# Format path representation portably
case "$INSTALL_DIR" in
  "$HOME"/*)
    REL_PATH="${INSTALL_DIR#"$HOME"/}"
    SHELL_PATH="\$HOME/$REL_PATH"
    LITERAL_PATH='$HOME/'"$REL_PATH"
    ;;
  *)
    SHELL_PATH="$INSTALL_DIR"
    LITERAL_PATH="$INSTALL_DIR"
    ;;
esac

modify_shell_rc() {
  local rc_file="$1"
  local export_line="$2"

  if [ -f "$rc_file" ] || [ -L "$rc_file" ]; then
    if grep -Fq "$LITERAL_PATH" "$rc_file" 2>/dev/null || grep -Fq "$INSTALL_DIR" "$rc_file" 2>/dev/null; then
      log_info "PATH entry already exists in ${BOLD}$rc_file${RESET}"
      CONFIGURED_ANY_RC=1
      return 0
    fi
    printf "\n# TermChat PATH\n%s\n" "$export_line" >> "$rc_file"
    log_success "Added TermChat to PATH in ${BOLD}$rc_file${RESET}"
    CONFIGURED_ANY_RC=1
  fi
}

check_path() {
  case ":$PATH:" in
    *":$INSTALL_DIR:"*|*":$INSTALL_DIR/:"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

log_step "Checking PATH configuration..."
CONFIGURED_ANY_RC=0

if check_path; then
  log_success "${BOLD}$INSTALL_DIR${RESET} is already in your PATH."
else
  if [ "$NO_MODIFY_PATH" -eq 1 ]; then
    log_info "Skipping PATH modification as requested (--no-modify-path)."
  else
    EXPORT_LINE="export PATH=\"$SHELL_PATH:\$PATH\""
    FISH_LINE="set -gx PATH $SHELL_PATH \$PATH"

    # Update relevant shell config files if they exist
    modify_shell_rc "$HOME/.bashrc" "$EXPORT_LINE"
    modify_shell_rc "$HOME/.zshrc" "$EXPORT_LINE"
    modify_shell_rc "$HOME/.bash_profile" "$EXPORT_LINE"
    modify_shell_rc "$HOME/.profile" "$EXPORT_LINE"

    # Fish shell support
    if [ -f "$HOME/.config/fish/config.fish" ]; then
      modify_shell_rc "$HOME/.config/fish/config.fish" "$FISH_LINE"
    fi

    # If no standard RC files were found, create ~/.bashrc or ~/.profile
    if [ "$CONFIGURED_ANY_RC" -eq 0 ]; then
      CURRENT_SHELL="$(basename "${SHELL:-bash}")"
      if [ "$CURRENT_SHELL" = "zsh" ]; then
        FALLBACK_RC="$HOME/.zshrc"
      else
        FALLBACK_RC="$HOME/.bashrc"
      fi
      printf "\n# TermChat PATH\n%s\n" "$EXPORT_LINE" >> "$FALLBACK_RC"
      log_success "Created and added TermChat to PATH in ${BOLD}$FALLBACK_RC${RESET}"
      CONFIGURED_ANY_RC=1
    fi
  fi
fi

# 7. Verification and final guidance
echo ""
echo "======================================"
log_success "${BOLD}TermChat ${DISPLAY_VERSION} installation complete!${RESET}"
echo ""

if ! check_path; then
  printf "%sTo use 'termchat' immediately in this terminal session, run:%s\n" "$YELLOW" "$RESET"
  printf "  %sexport PATH=\"%s:\$PATH\"%s\n" "$BOLD" "$INSTALL_DIR" "$RESET"
  echo ""
  printf "%sOr reload your shell configuration:%s\n" "$YELLOW" "$RESET"
  if [ -f "$HOME/.zshrc" ] && [ "$(basename "${SHELL:-}")" = "zsh" ]; then
    printf "  %ssource ~/.zshrc%s\n" "$BOLD" "$RESET"
  else
    printf "  %ssource ~/.bashrc%s\n" "$BOLD" "$RESET"
  fi
  echo ""
fi

printf "%s🚀 Get Started:%s\n" "$BOLD" "$RESET"
echo "  termchat"
echo ""
printf "%s💡 Connect to a custom relay:%s\n" "$BOLD" "$RESET"
echo "  termchat -server wss://your-relay.example.com/ws"
echo ""
