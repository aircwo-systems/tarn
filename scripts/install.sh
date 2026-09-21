#!/bin/sh
# Tarn installer — no admin rights required.
#
#   curl -fsSL https://aircwo-systems.github.io/tarn/install.sh | sh
#
# Environment overrides:
#   TARN_VERSION       release tag to install (default: latest), e.g. v0.2.0
#   TARN_INSTALL_DIR   install directory (default: $HOME/.tarn/bin)
#   TARN_NO_MODIFY_PATH=1  skip editing shell rc files
#
# Everything runs inside main() so a truncated download executes nothing.
set -eu

REPO="aircwo-systems/tarn"
BIN_NAME="tarn"

info() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed"; }

detect_platform() {
  os=$(uname -s)
  arch=$(uname -m)
  case "$os" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    MINGW* | MSYS* | CYGWIN*) die "Windows is not supported by this script; download the .zip from https://github.com/${REPO}/releases" ;;
    *) die "unsupported OS: $os" ;;
  esac
  case "$arch" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $arch" ;;
  esac
  # Rosetta: an x86_64 shell on Apple Silicon should still get the native build.
  if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
    arch=arm64
  fi
  printf '%s-%s' "$os" "$arch"
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

download() {
  curl --proto '=https' --tlsv1.2 -fsSL --retry 3 -o "$2" "$1"
}

# Append a PATH line to a shell rc file once. Tagged so reinstalls are idempotent.
add_to_rc() {
  rc="$1"
  line="$2"
  if [ -f "$rc" ] && grep -qF "$INSTALL_DIR" "$rc" 2>/dev/null; then
    return 0
  fi
  mkdir -p "$(dirname "$rc")"
  printf '\n# tarn\n%s\n' "$line" >>"$rc"
  info "  updated $rc"
}

configure_path() {
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) return 0 ;;
  esac
  if [ "${TARN_NO_MODIFY_PATH:-0}" = 1 ]; then
    warn "${INSTALL_DIR} is not on your PATH; add it manually"
    return 0
  fi

  posix_line="export PATH=\"${INSTALL_DIR}:\$PATH\""
  case "$(basename "${SHELL:-sh}")" in
    zsh)
      add_to_rc "${ZDOTDIR:-$HOME}/.zshrc" "$posix_line"
      ;;
    bash)
      # macOS Terminal opens login shells (.bash_profile); Linux opens interactive ones (.bashrc).
      add_to_rc "$HOME/.bashrc" "$posix_line"
      if [ -f "$HOME/.bash_profile" ]; then
        add_to_rc "$HOME/.bash_profile" "$posix_line"
      else
        add_to_rc "$HOME/.profile" "$posix_line"
      fi
      ;;
    fish)
      add_to_rc "${XDG_CONFIG_HOME:-$HOME/.config}/fish/conf.d/tarn.fish" "fish_add_path -g \"${INSTALL_DIR}\""
      ;;
    *)
      add_to_rc "$HOME/.profile" "$posix_line"
      ;;
  esac
  PATH_CHANGED=1
}

main() {
  need curl
  need tar
  need uname
  command -v sha256sum >/dev/null 2>&1 || need shasum

  VERSION="${TARN_VERSION:-latest}"
  INSTALL_DIR="${TARN_INSTALL_DIR:-$HOME/.tarn/bin}"
  PATH_CHANGED=0

  platform=$(detect_platform)
  asset="tarn-${platform}.tar.gz"
  if [ "$VERSION" = latest ]; then
    base="https://github.com/${REPO}/releases/latest/download"
  else
    base="https://github.com/${REPO}/releases/download/${VERSION}"
  fi

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT INT TERM

  info "Installing tarn (${VERSION}, ${platform})"
  download "${base}/${asset}" "${tmp}/${asset}" || die "download failed: ${base}/${asset}"

  if download "${base}/checksums.txt" "${tmp}/checksums.txt" 2>/dev/null; then
    expected=$(awk -v f="$asset" '$2 == f || $2 == "*"f {print $1}' "${tmp}/checksums.txt")
    [ -n "$expected" ] || die "no checksum for ${asset} in checksums.txt"
    actual=$(sha256_of "${tmp}/${asset}")
    [ "$expected" = "$actual" ] || die "checksum mismatch for ${asset} (expected ${expected}, got ${actual})"
    info "  checksum verified"
  else
    warn "release has no checksums.txt; skipping integrity check"
  fi

  tar -xzf "${tmp}/${asset}" -C "$tmp"
  # Current archives hold ./tarn; older ones hold raw-bins/tarn-<platform>.
  bin=$(find "$tmp" -type f \( -name "$BIN_NAME" -o -name "tarn-${platform}" \) | head -n 1)
  [ -n "$bin" ] || die "tarn binary not found in ${asset}"

  mkdir -p "$INSTALL_DIR"
  chmod 755 "$bin"
  # Atomic replace so a running tarn isn't corrupted mid-upgrade.
  mv -f "$bin" "${INSTALL_DIR}/${BIN_NAME}.new"
  mv -f "${INSTALL_DIR}/${BIN_NAME}.new" "${INSTALL_DIR}/${BIN_NAME}"
  info "  installed ${INSTALL_DIR}/${BIN_NAME}"

  configure_path

  existing=$(command -v "$BIN_NAME" 2>/dev/null || true)
  if [ -n "$existing" ] && [ "$existing" != "${INSTALL_DIR}/${BIN_NAME}" ]; then
    warn "another tarn at ${existing} shadows this install; remove it or reorder PATH"
  fi

  info ""
  if [ "$PATH_CHANGED" = 1 ]; then
    info "Open a new terminal (or run: export PATH=\"${INSTALL_DIR}:\$PATH\") then run: tarn start"
  else
    info "Done. Run: tarn start"
  fi
}

main "$@"
