#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

export GOCACHE="$ROOT/.cache/go-build"
OUT="$ROOT/dist/linux-amd64"
mkdir -p "$OUT"

go test ./...
find cmd internal configs scripts -type f \
  \( -name '*.go' -o -name '*.sql' -o -name '*.yaml' -o -name '*.yml' -o -name '*.sh' -o -name '*.bat' -o -name '*.ps1' \) |
while IFS= read -r file; do
  lines=$(wc -l < "$file")
  if [ "$lines" -gt 250 ]; then
    printf '%s\n' "文件行数超过 250 行：$file ($lines lines)"
    exit 1
  fi
done

REV=$(git rev-parse --short HEAD 2>/dev/null || printf '%s' unknown)
VERSION="dev-$REV"
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

go build -trimpath -ldflags "-X main.version=$VERSION" -o "$OUT/mirror-master" ./cmd/master
go build -trimpath -ldflags "-X main.version=$VERSION" -o "$OUT/mirror-node" ./cmd/node
rm -rf "$OUT/configs"
cp -R "$ROOT/configs" "$OUT/configs"

printf '%s\n' "Linux amd64 构建完成：$OUT"
