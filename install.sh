#!/usr/bin/env bash
# amux installer — downloads pre-built binary or builds from source automatically.
#
#   curl -fsSL https://raw.githubusercontent.com/ninhlee99/amux/main/install.sh | sh
#
# Supported OS: macOS (Apple Silicon arm64 & Intel amd64)
set -euo pipefail

REPO_URL="${AMUX_REPO_URL:-https://github.com/ninhlee99/amux.git}"

if [ -z "${AMUX_INSTALL_DIR:-}" ]; then
  if [ -d "$HOME/.local/bin" ] && [ -w "$HOME/.local/bin" ] && [[ ":$PATH:" == *":$HOME/.local/bin:"* ]]; then
    INSTALL_DIR="$HOME/.local/bin"
  elif [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
  fi
else
  INSTALL_DIR="$AMUX_INSTALL_DIR"
fi

say() { printf '%s\n' "$*" >&2; }
die() { say "install.sh: $*"; exit 1; }

[ "$(uname -s)" = "Darwin" ] || die "amux is macOS-only (uses the 'security' keychain CLI)."

ensure_git() {
  if command -v git >/dev/null 2>&1; then
    return 0
  fi
  say "git is not installed. Attempting auto-installation..."
  if command -v brew >/dev/null 2>&1; then
    say "installing git via Homebrew..."
    brew install git || die "failed to install git via Homebrew."
  else
    say "prompting Xcode Command Line Tools installation for git..."
    xcode-select --install 2>/dev/null || true
    die "git is required. Please complete the Xcode Command Line Tools installation and re-run."
  fi
}

ensure_go() {
  local need_install=0
  if ! command -v go >/dev/null 2>&1; then
    need_install=1
  else
    local ver
    ver="$(go version 2>/dev/null | awk '{print $3}' | sed 's/go//' || true)"
    local major minor
    major="$(echo "$ver" | cut -d. -f1)"
    minor="$(echo "$ver" | cut -d. -f2)"
    if [ "${major:-0}" -lt 1 ] || { [ "${major:-0}" -eq 1 ] && [ "${minor:-0}" -lt 22 ]; }; then
      say "current Go ($ver) is older than required 1.22+. Updating Go..."
      need_install=1
    fi
  fi

  if [ "$need_install" -eq 0 ]; then
    return 0
  fi

  say "Go is not installed or version < 1.22. Automatically installing Go..."

  # 1. Try Homebrew if available
  if command -v brew >/dev/null 2>&1; then
    say "installing Go via Homebrew (brew install go)..."
    brew install go || brew upgrade go || true
    if [ -x "/opt/homebrew/bin/go" ]; then
      export PATH="/opt/homebrew/bin:$PATH"
    elif [ -x "/usr/local/bin/go" ]; then
      export PATH="/usr/local/bin:$PATH"
    fi
    if command -v go >/dev/null 2>&1; then
      say "Go successfully installed via Homebrew: $(go version)"
      return 0
    fi
  fi

  # 2. Direct download official Go binary package from go.dev
  local arch
  arch="$(uname -m)"
  local go_arch
  case "$arch" in
    arm64|aarch64) go_arch="arm64" ;;
    x86_64|amd64)  go_arch="amd64" ;;
    *)             die "unsupported architecture: $arch" ;;
  esac

  local go_ver
  go_ver="$(curl -fsSL 'https://go.dev/VERSION?m=text' 2>/dev/null | head -n1 || echo "go1.23.6")"
  go_ver="$(echo "$go_ver" | tr -d '\r\n')"
  [ -n "$go_ver" ] || go_ver="go1.23.6"

  local go_tar="${go_ver}.darwin-${go_arch}.tar.gz"
  local go_url="https://go.dev/dl/${go_tar}"
  local go_target_dir="$HOME/.go"

  say "downloading official Go binary from ${go_url}..."
  mkdir -p "$tmp/godl"
  curl -fsSL "$go_url" -o "$tmp/godl/$go_tar" || die "failed to download Go from $go_url"

  say "extracting Go to $go_target_dir..."
  mkdir -p "$go_target_dir"
  rm -rf "$go_target_dir"/*
  tar -C "$go_target_dir" --strip-components=1 -xzf "$tmp/godl/$go_tar" || die "failed to extract Go package"

  export PATH="$go_target_dir/bin:$PATH"
  export GOROOT="$go_target_dir"

  # Permanently add to shell rc if not already present
  local rc_file=""
  if [ -n "${SHELL:-}" ]; then
    case "$SHELL" in
      */zsh)  rc_file="$HOME/.zshrc" ;;
      */bash) rc_file="$HOME/.bash_profile" ;;
    esac
  fi
  if [ -z "$rc_file" ] || [ ! -f "$rc_file" ]; then
    if [ -f "$HOME/.zshrc" ]; then
      rc_file="$HOME/.zshrc"
    elif [ -f "$HOME/.bash_profile" ]; then
      rc_file="$HOME/.bash_profile"
    elif [ -f "$HOME/.bashrc" ]; then
      rc_file="$HOME/.bashrc"
    fi
  fi

  if [ -n "$rc_file" ]; then
    if ! grep -q "$go_target_dir/bin" "$rc_file" 2>/dev/null; then
      printf '\n# Go binary (installed automatically by amux)\nexport GOROOT="%s"\nexport PATH="%s/bin:$PATH"\n' "$go_target_dir" "$go_target_dir" >> "$rc_file"
      say "added Go to $rc_file"
    fi
  fi

  if command -v go >/dev/null 2>&1; then
    say "Go successfully installed: $(go version)"
  else
    die "Go installation completed but 'go' binary not found in $go_target_dir/bin"
  fi
}

try_download_prebuilt() {
  local arch
  arch="$(uname -m)"
  local target_arch=""
  case "$arch" in
    arm64|aarch64) target_arch="arm64" ;;
    x86_64|amd64)  target_arch="amd64" ;;
    *)             return 1 ;;
  esac

  local bin_url="https://github.com/ninhlee99/amux/releases/latest/download/amux-darwin-${target_arch}"
  local dest="$tmp/amux-accounts/amux"

  say "checking for pre-built binary from GitHub Releases (${target_arch})..."
  # Test if binary URL exists on GitHub
  if curl -fsSL -I "$bin_url" 2>/dev/null | grep -q -E 'HTTP/.* (200|302)'; then
    say "downloading pre-built amux binary from GitHub..."
    if curl -fsSL "$bin_url" -o "$dest" 2>/dev/null && chmod +x "$dest"; then
      if "$dest" help >/dev/null 2>&1 || "$dest" -h >/dev/null 2>&1; then
        say "✓ Pre-built binary verified successfully (no Go compiler needed)."
        return 0
      fi
    fi
  fi
  return 1
}

tmp="$(mktemp -d)"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT
mkdir -p "$tmp/amux-accounts"

FORCE_BUILD=0
AUTO_UPDATE_FLAG=""
for arg in "$@"; do
  case "$arg" in
    --build|-b)
      FORCE_BUILD=1
      ;;
    --auto-update|-u)
      AUTO_UPDATE_FLAG="--auto-update"
      ;;
  esac
done
if [ "${AMUX_AUTO_UPDATE:-}" = "1" ]; then
  AUTO_UPDATE_FLAG="--auto-update"
fi

if [ -f "./cmd/amux/main.go" ] && [ -f "./go.mod" ]; then
  ensure_go
  say "building amux from local source..."
  go build -o "$tmp/amux-accounts/amux" ./cmd/amux || die "build failed."
elif [ -f "./main.go" ] && [ -f "./go.mod" ]; then
  ensure_go
  say "building amux from local source..."
  go build -o "$tmp/amux-accounts/amux" . || die "build failed."
else
  installed_prebuilt=0
  if [ "$FORCE_BUILD" -eq 0 ]; then
    if try_download_prebuilt; then
      installed_prebuilt=1
    else
      say "pre-built binary not found on GitHub Release. Falling back to build from source..."
    fi
  fi

  if [ "$installed_prebuilt" -eq 0 ]; then
    ensure_git
    ensure_go
    say "cloning $REPO_URL..."
    git clone --depth 1 "$REPO_URL" "$tmp/amux-accounts/src" >/dev/null 2>&1 \
      || die "clone failed — check the URL and your network."

    say "building amux..."
    ( cd "$tmp/amux-accounts/src" && go build -o "$tmp/amux-accounts/amux" ./cmd/amux ) \
      || ( cd "$tmp/amux-accounts/src" && go build -o "$tmp/amux-accounts/amux" . ) \
      || die "build failed."
  fi
fi

if [ -w "$INSTALL_DIR" ]; then
  mv "$tmp/amux-accounts/amux" "$INSTALL_DIR/amux"
else
  say "need sudo to write to $INSTALL_DIR..."
  sudo mv "$tmp/amux-accounts/amux" "$INSTALL_DIR/amux"
fi

if [ "$INSTALL_DIR" != "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
  cp "$INSTALL_DIR/amux" "/usr/local/bin/amux" 2>/dev/null || true
fi

say "✓ Installed amux -> $INSTALL_DIR/amux"

if command -v amux >/dev/null 2>&1; then
  say "running 'amux setup $AUTO_UPDATE_FLAG'..."
  amux setup $AUTO_UPDATE_FLAG || say "amux setup reported an issue — you can re-run it any time: amux setup $AUTO_UPDATE_FLAG"
else
  say "warning: $INSTALL_DIR isn't on PATH — add it, then run: amux setup $AUTO_UPDATE_FLAG"
fi

say ""
say "Installation complete. Next: amux id add [provider] (register your accounts)"
