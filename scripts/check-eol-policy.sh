#!/usr/bin/env bash

set -u
export LC_ALL=C

fatal() {
  printf 'eol-policy: error=%s\n' "$1" >&2
  exit 2
}

for tool in git tr cmp mktemp rm; do
  command -v "$tool" >/dev/null 2>&1 || fatal "required tool unavailable: $tool"
done

root=$(git rev-parse --show-toplevel 2>/dev/null) || fatal 'not a Git worktree'
root=$(cd "$root" && pwd -P) || fatal 'cannot resolve Git worktree root'
here=$(pwd -P) || fatal 'cannot resolve current directory'
[[ "$here" == "$root" ]] || fatal 'must run from the Git worktree root'

tmp=$(mktemp -d "${TMPDIR:-/tmp}/check-eol-policy.XXXXXX") || fatal 'cannot create temporary directory'
cleanup() {
  status=$?
  trap - EXIT
  if ! rm -rf "$tmp"; then
    printf 'eol-policy: error=failed to remove temporary directory\n' >&2
    status=2
  fi
  exit "$status"
}
trap cleanup EXIT

report() {
  printf 'eol-policy: path=%q defect=%s\n' "$1" "$2" >&2
  violations=1
}

check_byte() {
  file=$1
  byte=$2
  path=$3
  defect=$4
  tr -d "$byte" < "$file" > "$tmp/stripped" || fatal 'tr failed while inspecting content'
  cmp -s "$file" "$tmp/stripped"
  status=$?
  [[ "$status" -eq 0 ]] && return
  [[ "$status" -eq 1 ]] || fatal 'cmp failed while inspecting content'
  report "$path" "$defect"
}

if ! git ls-files --stage -z > "$tmp/index"; then
  fatal 'git ls-files failed'
fi

violations=0
while IFS= read -r -d '' entry; do
  [[ "$entry" == *$'\t'* ]] || fatal 'malformed git ls-files record'
  metadata=${entry%%$'\t'*}
  path=${entry#*$'\t'}
  read -r mode oid stage extra <<< "$metadata"
  [[ -n "$mode" && -n "$oid" && -n "$stage" && -z "${extra:-}" ]] || fatal 'malformed git ls-files metadata'
  [[ "$stage" == 0 ]] || continue

  case "$path" in
    *.go|*.ts|*.mod|*.sum|*.md|*.json|*.toml|*.golden|*.yml|*.yaml|*.sh|*.ps1|Makefile|*/Makefile|.gitignore|*/.gitignore|.gitattributes|*/.gitattributes) ;;
    *) continue ;;
  esac

  if ! git check-attr --cached -z text eol -- "$path" > "$tmp/attrs"; then
    fatal 'git check-attr failed'
  fi
  fields=()
  while IFS= read -r -d '' field; do
    fields[${#fields[@]}]=$field
  done < "$tmp/attrs"
  [[ ${#fields[@]} -eq 6 && "${fields[0]}" == "$path" && "${fields[1]}" == text && "${fields[3]}" == "$path" && "${fields[4]}" == eol ]] || fatal 'malformed git check-attr output'
  [[ "${fields[2]}" == set ]] || report "$path" "attribute text is '${fields[2]}', expected 'set'"
  [[ "${fields[5]}" == lf ]] || report "$path" "attribute eol is '${fields[5]}', expected 'lf'"

  git cat-file blob "$oid" > "$tmp/blob" || fatal 'git cat-file failed'
  check_byte "$tmp/blob" '\015' "$path" 'CR byte in indexed blob'
  check_byte "$tmp/blob" '\000' "$path" 'NUL byte in indexed blob'

  if [[ ! -f "$path" || -L "$path" ]]; then
    report "$path" 'worktree content is missing or not a regular file'
    continue
  fi
  check_byte "$path" '\015' "$path" 'CR byte in regular worktree file'
  check_byte "$path" '\000' "$path" 'NUL byte in regular worktree file'
done < "$tmp/index"

exit "$violations"
