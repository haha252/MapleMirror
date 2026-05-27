#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

export GOCACHE="$ROOT/.cache/go-build"
OUT="$ROOT/dist/linux-amd64"
mkdir -p "$OUT"

go test ./...

REV=$(git rev-parse --short HEAD 2>/dev/null || printf '%s' unknown)
VERSION="M1-$REV"
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0

go build -trimpath -ldflags "-X main.version=$VERSION" -o "$OUT/mirror-master" ./cmd/master
go build -trimpath -ldflags "-X main.version=$VERSION" -o "$OUT/mirror-node" ./cmd/node
rm -rf "$OUT/configs"
cp -R "$ROOT/configs" "$OUT/configs"

printf '%s\n' "Linux amd64 构建完成：$OUT"
