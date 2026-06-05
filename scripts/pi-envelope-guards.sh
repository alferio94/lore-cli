#!/usr/bin/env bash
# scripts/pi-envelope-guards.sh — static grep guards for the canonical Pi Lore delegation
# envelope contract. Scans both `lore-cli/` and `lore-pi-runtime/` and the installed
# `~/.pi/agent/` config to detect drift in stale keys, legacy delegation-note drift, and
# forbidden canonical-status patterns.
#
# Usage:
#   scripts/pi-envelope-guards.sh [mode]
#
# Modes:
#   repo      (default) — scan both `lore-cli/` and `lore-pi-runtime/` source trees.
#   installed           — scan `~/.pi/agent/` for stale installed config.
#   all                 — scan both `repo` and `installed` modes.
#
# Exit codes:
#   0  no forbidden patterns found.
#   1  at least one forbidden pattern was found; offending file:line is printed.
#   2  usage / environment error.

set -uo pipefail

MODE="${1:-repo}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNTIME_DIR="$(cd "$WORKSPACE_DIR/../lore-pi-runtime" 2>/dev/null && pwd || true)"
INSTALLED_DIR="$HOME/.pi/agent"

if [[ -z "$RUNTIME_DIR" || ! -d "$RUNTIME_DIR" ]]; then
  echo "ERROR: could not locate sibling lore-pi-runtime repository at $WORKSPACE_DIR/../lore-pi-runtime" >&2
  exit 2
fi

# Forbidden patterns. Each pattern is followed by a human-readable name. Patterns are
# intentionally conservative: the wording must be a literal canonical-statement marker
# (e.g. "Final output status must be one of:"), not just any word like `next` or
# `executive_summary` which appear in legitimate "do not use" warnings.
#
# Order: (file-glob, pattern, label). The grep is anchored to a single line so accidental
# substrings (e.g. inside a longer string) still match.
declare -a FORBIDDEN=(
  # Forbidden canonical-field-list patterns
  '*.{go,ts,md,markdown}|exactly these keys: `status`, `summary`, `artifacts`, `next`,|old next-only worker key list'
  '*.{go,ts,md,markdown}|exactly these keys: `status`, `summary`, `artifacts`, `next`, `continuation`|old next+continuation worker key list'
  '*.{go,ts,md,markdown}|envelope with keys `status`, `phase`, `summary`, `artifacts`, `next`|old SDD envelope with next'
  '*.{go,ts,md,markdown}|envelope with keys `status`, `phase`, `summary`, `artifacts`, `next`, `continuation`, `question`, `options`, `risks`, `skill_resolution`|old envelope with next (alt form)'
  # Forbidden canonical-status patterns
  '*.{go,ts,md,markdown}|`status`: `completed` | `running` | `needs_user_input` | `failed`|old status-with-running pattern'
  '*.{go,ts,md,markdown}|Final output status must be one of: `completed`, `running`, `needs_user_input`, `failed`|old final-status-with-running pattern'
  '*.{go,ts,md,markdown}|`status` must be one of: completed, running, needs_user_input, failed|old running-included status wording'
  # Forbidden legacy delegation ownership notes
  '*.{go,ts,md,markdown}|delegation is provided by the legacy `lore-delegation` extension|legacy delegation ownership claim'
  '*.{go,ts,md,markdown}|the `lore-delegation` Pi extension is active|legacy delegation extension active claim'
  '*.{go,ts,md,markdown}|Delegation is provided by the `lore-delegation` extension|legacy delegation extension active claim (capitalized)'
)

scan_repo() {
  local scope="$1"
  local target="$2"
  if [[ ! -d "$target" ]]; then
    echo "WARN: skipping $scope scan: $target is not a directory" >&2
    return 0
  fi
  echo "Scanning $scope: $target"
  local violations=0
  while IFS='|' read -r glob pattern label; do
    [[ -z "$glob" ]] && continue
    # Use grep -RInE with --include to filter by glob. The pattern is a fixed string,
    # so we escape it for grep -E and use -- to prevent flag injection.
    # We exclude node_modules, .git, dist, build, and other typical noise directories.
    while IFS=: read -r file line _; do
      [[ -z "$file" ]] && continue
      violations=$((violations + 1))
      echo "  FORBIDDEN: $label"
      echo "    file: $file:$line"
      echo "    pattern: $pattern"
    done < <(
      grep -RInF --include="$glob" \
        --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist --exclude-dir=build \
        --exclude-dir=coverage --exclude-dir=.pi --exclude-dir=backups \
        -- "$pattern" "$target" 2>/dev/null
    )
  done < <(printf '%s\n' "${FORBIDDEN[@]}" | awk -F'|' 'NF==3 { print $1 "|" $2 "|" $3 }')
  return $violations
}

scan_installed() {
  local target="$INSTALLED_DIR"
  if [[ ! -d "$target" ]]; then
    echo "WARN: skipping installed scan: $target is not a directory" >&2
    return 0
  fi
  echo "Scanning installed: $target"
  local violations=0
  while IFS='|' read -r glob pattern label; do
    [[ -z "$glob" ]] && continue
    # Limit the installed scan to .md files for the Pi envelope-related patterns; the
    # installed config is all markdown plus settings.json. We exclude auth.json, settings.json,
    # and the lore-store files (mcp-cache.json, lore-install.json) from the forbidden scan
    # because they are user data and may contain arbitrary strings.
    while IFS=: read -r file line _; do
      [[ -z "$file" ]] && continue
      # Skip user data files.
      case "$file" in
        */auth.json|*/settings.json|*/mcp-cache.json|*/lore-install.json|*/mcp.json) continue ;;
      esac
      violations=$((violations + 1))
      echo "  FORBIDDEN: $label"
      echo "    file: $file:$line"
      echo "    pattern: $pattern"
    done < <(
      grep -RInF --include="$glob" \
        --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=sessions \
        --exclude-dir=backups --exclude-dir=npm --exclude-dir=git --exclude-dir=lore \
        --exclude-dir=extensions --exclude-dir=extensions-disabled --exclude-dir=themes \
        --exclude-dir=prompts --exclude-dir=skills \
        -- "$pattern" "$target" 2>/dev/null
    )
  done < <(printf '%s\n' "${FORBIDDEN[@]}" | awk -F'|' 'NF==3 { print $1 "|" $2 "|" $3 }')
  return $violations
}

case "$MODE" in
  repo)
    scan_repo "lore-cli" "$WORKSPACE_DIR"
    scan_repo "lore-pi-runtime" "$RUNTIME_DIR"
    ;;
  installed)
    scan_installed
    ;;
  all)
    scan_repo "lore-cli" "$WORKSPACE_DIR"
    scan_repo "lore-pi-runtime" "$RUNTIME_DIR"
    scan_installed
    ;;
  *)
    echo "ERROR: unknown mode '$MODE' (expected: repo | installed | all)" >&2
    exit 2
    ;;
esac

echo "OK: no forbidden Pi envelope patterns found in mode '$MODE'."
exit 0
