#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

export GOCACHE="$ROOT/.cache/go-build"
OUT="$ROOT/dist/linux-amd64"
mkdir -p "$OUT"

go test ./...
if command -v clang >/dev/null 2>&1; then
  clang --target=wasm32 -O3 -nostdlib \
    "-Wl,--no-entry" "-Wl,--export-memory" "-Wl,--export=get_buffer" \
    "-Wl,--export=solve_pow" "-Wl,--initial-memory=2097152" "-Wl,--max-memory=2097152" \
    -o "$ROOT/web/public/static/pow.wasm" "$ROOT/web/wasm/pow.c"
elif command -v zig >/dev/null 2>&1; then
  zig cc -target wasm32-freestanding -O3 -nostdlib \
    "-Wl,--no-entry" "-Wl,--export-memory" "-Wl,--export=get_buffer" \
    "-Wl,--export=solve_pow" "-Wl,--initial-memory=2097152" "-Wl,--max-memory=2097152" \
    -o "$ROOT/web/public/static/pow.wasm" "$ROOT/web/wasm/pow.c"
else
  printf '%s\n' "未找到 clang 或 zig，跳过网页 PoW WASM 构建，将使用浏览器 JS 回退。"
fi
find cmd internal configs scripts web -type f \
  \( -name '*.go' -o -name '*.sql' -o -name '*.yaml' -o -name '*.yml' -o -name '*.sh' -o -name '*.bat' -o -name '*.ps1' -o -name '*.js' -o -name '*.css' -o -name '*.html' \) |
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
rm -rf "$OUT/web"

printf '%s\n' "Linux amd64 构建完成：$OUT"
