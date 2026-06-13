#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if sh "$ROOT/scripts/count-lines.sh" --max-per-file 250 --quiet; then
  printf '%s\n' "文件行数检查通过：源码和前端资源均未超过 250 行。"
else
  exit 1
fi
