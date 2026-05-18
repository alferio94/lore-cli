#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_DIR="$(mktemp -d)"
FIXTURE_VERSION="v9.9.9"
BIN_NAME="lore"
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

require_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "$haystack" != *"$needle"* ]]; then
    echo "expected output to contain: $needle" >&2
    exit 1
  fi
}

mkdir -p "$WORK_DIR/releases/$FIXTURE_VERSION" "$WORK_DIR/build"

HOST_OS_RAW="$(uname -s)"
HOST_ARCH_RAW="$(uname -m)"
case "$HOST_OS_RAW" in
  Darwin) HOST_OS="darwin" ;;
  Linux) HOST_OS="linux" ;;
  *) echo "unsupported host OS for smoke test: $HOST_OS_RAW" >&2; exit 1 ;;
esac
case "$HOST_ARCH_RAW" in
  x86_64|amd64) HOST_ARCH="amd64" ;;
  arm64|aarch64) HOST_ARCH="arm64" ;;
  *) echo "unsupported host arch for smoke test: $HOST_ARCH_RAW" >&2; exit 1 ;;
esac

pushd "$ROOT_DIR" >/dev/null
GOFLAGS="" go build -trimpath \
  -ldflags "-X github.com/alferio94/lore-cli/internal/version.Version=$FIXTURE_VERSION -X github.com/alferio94/lore-cli/internal/version.Commit=test -X github.com/alferio94/lore-cli/internal/version.BuildDate=test" \
  -o "$WORK_DIR/build/$BIN_NAME" ./cmd/lore
popd >/dev/null

cp "$WORK_DIR/build/$BIN_NAME" "$WORK_DIR/releases/$FIXTURE_VERSION/$BIN_NAME"
UNIX_ARCHIVE="lore-cli_${FIXTURE_VERSION}_${HOST_OS}_${HOST_ARCH}.tar.gz"
tar -C "$WORK_DIR/releases/$FIXTURE_VERSION" -czf "$WORK_DIR/releases/$FIXTURE_VERSION/$UNIX_ARCHIVE" "$BIN_NAME"
if command -v zip >/dev/null 2>&1; then
  (cd "$WORK_DIR/releases/$FIXTURE_VERSION" && cp "$BIN_NAME" lore.exe && zip -q "lore-cli_${FIXTURE_VERSION}_windows_${HOST_ARCH}.zip" lore.exe && rm -f lore.exe)
fi
(
  cd "$WORK_DIR/releases/$FIXTURE_VERSION"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$UNIX_ARCHIVE" > SHA256SUMS
    if [[ -f "lore-cli_${FIXTURE_VERSION}_windows_${HOST_ARCH}.zip" ]]; then
      sha256sum "lore-cli_${FIXTURE_VERSION}_windows_${HOST_ARCH}.zip" >> SHA256SUMS
    fi
  else
    shasum -a 256 "$UNIX_ARCHIVE" > SHA256SUMS
    if [[ -f "lore-cli_${FIXTURE_VERSION}_windows_${HOST_ARCH}.zip" ]]; then
      shasum -a 256 "lore-cli_${FIXTURE_VERSION}_windows_${HOST_ARCH}.zip" >> SHA256SUMS
    fi
  fi
)

UNIX_INSTALL_DIR="$WORK_DIR/unix-bin"
DEFAULT_HOME="$WORK_DIR/home-default"
mkdir -p "$DEFAULT_HOME"
DEFAULT_OUTPUT="$(HOME="$DEFAULT_HOME" LORE_INSTALL_BASE_URL="file://$WORK_DIR/releases" sh "$ROOT_DIR/scripts/install.sh" --version "$FIXTURE_VERSION" --bin-dir "$UNIX_INSTALL_DIR" 2>&1)"
"$UNIX_INSTALL_DIR/lore" version | grep -F "$FIXTURE_VERSION" >/dev/null
require_contains "$DEFAULT_OUTPUT" "Run it directly now: $UNIX_INSTALL_DIR/lore"
require_contains "$DEFAULT_OUTPUT" "PATH is unchanged by default. Optional PATH opt-in later: rerun with --add-to-path or add this line to ${DEFAULT_HOME}/.profile:"
if [[ -f "$DEFAULT_HOME/.profile" ]] && grep -Fq "export PATH=\"${UNIX_INSTALL_DIR}:\$PATH\"" "$DEFAULT_HOME/.profile"; then
  echo "default installer run unexpectedly modified ${DEFAULT_HOME}/.profile" >&2
  exit 1
fi

PATH_HOME="$WORK_DIR/home-add"
PATH_INSTALL_DIR="$WORK_DIR/unix-bin-add"
PATH_LINE="export PATH=\"${PATH_INSTALL_DIR}:\$PATH\""
mkdir -p "$PATH_HOME"
ADD_OUTPUT="$(HOME="$PATH_HOME" LORE_INSTALL_BASE_URL="file://$WORK_DIR/releases" sh "$ROOT_DIR/scripts/install.sh" --version "$FIXTURE_VERSION" --bin-dir "$PATH_INSTALL_DIR" --add-to-path 2>&1)"
"$PATH_INSTALL_DIR/lore" version | grep -F "$FIXTURE_VERSION" >/dev/null
require_contains "$ADD_OUTPUT" "Run it directly now: $PATH_INSTALL_DIR/lore"
require_contains "$ADD_OUTPUT" "Added PATH entry to ${PATH_HOME}/.profile. Open a new terminal/session to run 'lore' by name."
grep -Fqx "$PATH_LINE" "$PATH_HOME/.profile" >/dev/null
[[ "$(grep -Fxc "$PATH_LINE" "$PATH_HOME/.profile")" -eq 1 ]]

ADD_REPEAT_OUTPUT="$(HOME="$PATH_HOME" LORE_INSTALL_BASE_URL="file://$WORK_DIR/releases" sh "$ROOT_DIR/scripts/install.sh" --version "$FIXTURE_VERSION" --bin-dir "$PATH_INSTALL_DIR" --add-to-path 2>&1)"
require_contains "$ADD_REPEAT_OUTPUT" "PATH entry already present in ${PATH_HOME}/.profile."
require_contains "$ADD_REPEAT_OUTPUT" "Open a new terminal/session if 'lore' is not available in this shell yet."
[[ "$(grep -Fxc "$PATH_LINE" "$PATH_HOME/.profile")" -eq 1 ]]

if command -v pwsh >/dev/null 2>&1 && command -v zip >/dev/null 2>&1; then
  PW_INSTALL_DIR="$WORK_DIR/windows-bin"
  PW_OUTPUT="$(pwsh -NoLogo -NoProfile -File "$ROOT_DIR/scripts/install.ps1" -Version "$FIXTURE_VERSION" -BaseUrl "file://$WORK_DIR/releases" -InstallDir "$PW_INSTALL_DIR" -PlatformArchOverride "$HOST_ARCH" 2>&1)"
  "$PW_INSTALL_DIR/lore.exe" version | grep -F "$FIXTURE_VERSION" >/dev/null
  require_contains "$PW_OUTPUT" "Run it directly now: $PW_INSTALL_DIR/lore.exe"
  require_contains "$PW_OUTPUT" "PATH is unchanged by default. Optional PATH opt-in later: rerun install.ps1 -AddToPath or add $PW_INSTALL_DIR to your user PATH."
fi

echo "installer smoke tests passed"
