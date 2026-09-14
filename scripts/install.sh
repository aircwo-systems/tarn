#!/usr/bin/env bash
# Install tarn (darwin-arm64) from the latest GitHub release, no admin required.
set -euo pipefail

REPO="aircwo-systems/tarn"
ASSET="tarn-darwin-arm64.tar.gz"
BIN_NAME="tarn"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${TARN_INSTALL_DIR:-$HOME/.local/bin}"

if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
else
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

echo "Downloading ${ASSET} (${VERSION})..."
curl -fsSL -o "${tmp_dir}/${ASSET}" "$URL"

echo "Extracting..."
tar -xzf "${tmp_dir}/${ASSET}" -C "$tmp_dir"

extracted_bin="$(find "$tmp_dir" -type f -name 'tarn-darwin-arm64' -print -quit)"
if [ -z "$extracted_bin" ]; then
  echo "error: expected binary not found in archive (tarn-darwin-arm64)" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
mv "$extracted_bin" "${INSTALL_DIR}/${BIN_NAME}"

chmod +x "${INSTALL_DIR}/${BIN_NAME}"
xattr -d com.apple.quarantine "${INSTALL_DIR}/${BIN_NAME}" 2>/dev/null || true

echo "Installed ${BIN_NAME} to ${INSTALL_DIR}/${BIN_NAME}"

# --- Add INSTALL_DIR to PATH (user scope, no admin needed) ---
add_path_line() {
  local rc_file="$1"
  local line="export PATH=\"${INSTALL_DIR}:\$PATH\""
  [ -f "$rc_file" ] || touch "$rc_file"
  if ! grep -qF "$INSTALL_DIR" "$rc_file" 2>/dev/null; then
    { echo ""; echo "# Added by tarn install script"; echo "$line"; } >> "$rc_file"
    echo "Updated $rc_file"
  fi
}

case "$(basename "${SHELL:-}")" in
  zsh)
    add_path_line "$HOME/.zshrc"
    ;;
  bash)
    add_path_line "$HOME/.bash_profile"
    ;;
  *)
    add_path_line "$HOME/.profile"
    ;;
esac

case ":$PATH:" in
  *":${INSTALL_DIR}:"*)
    echo "${BIN_NAME} ready — run '${BIN_NAME} --help'"
    ;;
  *)
    echo "Restart your shell (or run 'source ~/.zshrc') then run '${BIN_NAME} --help'"
    ;;
esac
