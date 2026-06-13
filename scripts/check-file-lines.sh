#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TMPFILE=$(mktemp "${TMPDIR:-/tmp}/mirror-server-check-file-lines.XXXXXX")
trap 'rm -f "$TMPFILE"' EXIT INT HUP TERM

find "$ROOT" \
  \( -path "$ROOT/.git" -o -path "$ROOT/.git/*" -o \
     -path "$ROOT/.cache" -o -path "$ROOT/.cache/*" -o \
     -path "$ROOT/dist" -o -path "$ROOT/dist/*" \) -prune -o \
  -type f \( \
    -name '*.go' -o -name '*.sql' -o -name '*.yaml' -o -name '*.yml' -o \
    -name '*.ps1' -o -name '*.bat' -o -name '*.sh' -o -name '*.js' -o \
    -name '*.css' -o -name '*.html' \
  \) -print |
while IFS= read -r file; do
  lines=$(wc -l < "$file")
  if [ "$lines" -gt 250 ]; then
    rel=${file#"$ROOT"/}
    printf '%s (%s lines)\n' "$rel" "$lines" >> "$TMPFILE"
  fi
done

if [ -s "$TMPFILE" ]; then
  printf '%s\n' "以下源码或资源文件超过 250 行，请按职责拆分："
  cat "$TMPFILE"
  exit 1
fi

printf '%s\n' "文件行数检查通过：源码和前端资源均未超过 250 行。"
