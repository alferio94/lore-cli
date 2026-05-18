#!/usr/bin/env sh
set -eu

DEFAULT_VERSION="__DEFAULT_VERSION__"
REPO_SLUG="alferio94/lore-cli"
BINARY_NAME="lore"
BIN_DIR="${HOME}/.local/bin"
ADD_TO_PATH=0
FORCE=0
VERSION=""
BASE_URL="${LORE_INSTALL_BASE_URL:-}"

usage() {
  cat <<EOF
Install lore-cli from GitHub Releases.

Usage: install.sh [--version <tag|latest>] [--bin-dir <dir>] [--add-to-path] [--force] [--help]

Defaults:
  version: embedded release tag (${DEFAULT_VERSION})
  bin dir: ${HOME}/.local/bin

Notes:
  - Pinned release asset URLs are the recommended install path.
  - Checksums provide integrity verification only; signing/notarization is out of scope.
  - Config under the user config directory is preserved by reinstall/uninstall.
EOF
}

log() {
  printf '%s\n' "$*" >&2
}

fail() {
  log "error: $*"
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

resolve_version() {
  requested="$1"
  if [ -z "$requested" ]; then
    requested="$DEFAULT_VERSION"
  fi
  if [ "$requested" = "latest" ]; then
    need_cmd curl
    effective_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO_SLUG}/releases/latest")" || fail "failed to resolve latest release; rerun with --version <tag>"
    tag="$(basename "$effective_url")"
    case "$tag" in
      v*) printf '%s\n' "$tag" ;;
      *) fail "latest release resolved ambiguously (${effective_url}); rerun with --version <tag>" ;;
    esac
    return
  fi
  case "$requested" in
    v*) printf '%s\n' "$requested" ;;
    *) fail "version must be a release tag like v1.2.3 or the literal latest" ;;
  esac
}

release_base_url() {
  version="$1"
  if [ -n "$BASE_URL" ]; then
    printf '%s/%s\n' "$(printf '%s' "$BASE_URL" | sed 's:/*$::')" "$version"
  else
    printf 'https://github.com/%s/releases/download/%s\n' "$REPO_SLUG" "$version"
  fi
}

resolve_platform() {
  os_raw="$(uname -s 2>/dev/null || true)"
  arch_raw="$(uname -m 2>/dev/null || true)"
  case "$os_raw" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) fail "unsupported operating system: ${os_raw:-unknown}" ;;
  esac
  case "$arch_raw" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) fail "unsupported architecture: ${arch_raw:-unknown}" ;;
  esac
  printf '%s %s tar.gz\n' "$os" "$arch"
}

download() {
  url="$1"
  output="$2"
  need_cmd curl
  curl -fsSL "$url" -o "$output"
}

sha256_file() {
  file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    fail "required command not found: sha256sum or shasum"
  fi
}

verify_checksum() {
  sums_file="$1"
  archive_file="$2"
  archive_name="$(basename "$archive_file")"
  expected="$(awk -v file="$archive_name" '$2==file {print $1}' "$sums_file")"
  [ -n "$expected" ] || fail "missing checksum for ${archive_name}"
  actual="$(lower "$(sha256_file "$archive_file")")"
  if [ "$actual" != "$(lower "$expected")" ]; then
    fail "checksum mismatch for ${archive_name}"
  fi
}

extract_archive() {
  archive_file="$1"
  destination="$2"
  need_cmd tar
  tar -xzf "$archive_file" -C "$destination"
  [ -f "$destination/$BINARY_NAME" ] || fail "archive did not contain ${BINARY_NAME}"
}

install_binary() {
  extracted_binary="$1"
  target_dir="$2"
  mkdir -p "$target_dir"
  target_path="$target_dir/$BINARY_NAME"
  temp_target="$target_path.tmp.$$"
  cp "$extracted_binary" "$temp_target"
  chmod 0755 "$temp_target"
  mv -f "$temp_target" "$target_path"
  printf '%s\n' "$target_path"
}

verify_install() {
  binary_path="$1"
  "$binary_path" version >/dev/null || fail "installed binary failed version check"
}

append_path_line() {
  rc_file="$1"
  path_line="$2"
  if [ ! -f "$rc_file" ]; then
    : > "$rc_file"
  fi
  if grep -Fq "$path_line" "$rc_file"; then
    log "PATH entry already present in ${rc_file}"
    return
  fi
  printf '\n%s\n' "$path_line" >> "$rc_file"
  log "Added PATH guidance to ${rc_file}; restart your shell after installation."
}

handle_path() {
  target_dir="$1"
  case ":$PATH:" in
    *:"$target_dir":*)
      log "${target_dir} is already on PATH."
      return
      ;;
  esac

  if [ "$ADD_TO_PATH" -eq 1 ]; then
    rc_file="${HOME}/.profile"
    path_line="export PATH=\"${target_dir}:\$PATH\""
    append_path_line "$rc_file" "$path_line"
  else
    log "Add ${target_dir} to PATH to run 'lore' without a full path:"
    log "  export PATH=\"${target_dir}:\$PATH\""
  fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || fail "--version requires a value"
      VERSION="$2"
      shift 2
      ;;
    --bin-dir)
      [ "$#" -ge 2 ] || fail "--bin-dir requires a value"
      BIN_DIR="$2"
      shift 2
      ;;
    --add-to-path)
      ADD_TO_PATH=1
      shift
      ;;
    --force)
      FORCE=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

VERSION="$(resolve_version "$VERSION")"
set -- $(resolve_platform)
OS="$1"
ARCH="$2"
ARCHIVE_EXT="$3"
ARCHIVE_NAME="lore-cli_${VERSION}_${OS}_${ARCH}.${ARCHIVE_EXT}"
RELEASE_BASE_URL="$(release_base_url "$VERSION")"
TMPDIR="$(mktemp -d 2>/dev/null || mktemp -d -t lore-install)"
cleanup() {
  rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM HUP

ARCHIVE_PATH="$TMPDIR/$ARCHIVE_NAME"
SUMS_PATH="$TMPDIR/SHA256SUMS"
DOWNLOAD_BASE="$RELEASE_BASE_URL"

if [ "$FORCE" -eq 1 ]; then
  log "Force mode enabled; existing binaries will be replaced if present."
fi

log "Installing lore ${VERSION} for ${OS}/${ARCH}"
download "$DOWNLOAD_BASE/$ARCHIVE_NAME" "$ARCHIVE_PATH" || fail "failed to download ${ARCHIVE_NAME}"
download "$DOWNLOAD_BASE/SHA256SUMS" "$SUMS_PATH" || fail "failed to download SHA256SUMS"
verify_checksum "$SUMS_PATH" "$ARCHIVE_PATH"
extract_archive "$ARCHIVE_PATH" "$TMPDIR"
TARGET_PATH="$(install_binary "$TMPDIR/$BINARY_NAME" "$BIN_DIR")"
verify_install "$TARGET_PATH"
handle_path "$BIN_DIR"
log "Installed ${TARGET_PATH}"
log "Uninstall: delete ${TARGET_PATH}; config under your user config directory is preserved by default."
