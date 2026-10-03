#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  master) role=master; db_name=master.db ;;
  node) role=node; db_name=node-state.db ;;
  *) echo "Usage: sudo bash deploy.sh master|node" >&2; exit 2 ;;
esac
[[ $(id -u) == 0 ]] || { echo "请使用 root 或 sudo 执行" >&2; exit 1; }
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || { echo "此安装包仅支持 Linux amd64" >&2; exit 1; }

bundle=$(cd -- "$(dirname -- "$0")" && pwd)
prefix="/opt/mirror-$role"
unit="mirror-$role.service"
binary="$prefix/mirror-$role"
db_path="$prefix/data/$db_name"
[[ -f "$binary" && -f "$db_path" ]] || { echo "未找到预期程序或数据库：$prefix" >&2; exit 1; }
exec_start=$(systemctl show --value -p ExecStart "$unit")
[[ "$exec_start" == *"$binary"* ]] || { echo "服务启动路径不匹配，请先检查 $unit 的 ExecStart" >&2; exit 1; }
(cd "$bundle" && sha256sum -c SHA256SUMS)
command -v python3 >/dev/null

stamp=$(date -u +%Y%m%dT%H%M%SZ)
backup="$prefix/backups/sync-recovery-$stamp"
mkdir -p "$backup"
cp -p "$binary" "$backup/mirror-$role"
install -m 0755 "$bundle/mirror-$role" "$binary.sync-recovery-new"
systemctl stop "$unit"
# If backup or installation fails, bring the existing service back up.
trap 'systemctl start "$unit" >/dev/null 2>&1 || true' EXIT
python3 - "$db_path" "$backup/$db_name" <<'PY'
import sqlite3, sys
from pathlib import Path
source = sqlite3.connect(Path(sys.argv[1]).resolve().as_uri() + "?mode=ro", uri=True)
target = sqlite3.connect(sys.argv[2])
try:
    source.backup(target)
finally:
    target.close()
    source.close()
PY
mv -f "$binary.sync-recovery-new" "$binary"
systemctl start "$unit"
systemctl is-active --quiet "$unit"
trap - EXIT
echo "安装完成，备份目录：$backup"
echo "查看日志：journalctl -u $unit --since '5 minutes ago' --no-pager -n 100"
echo "回滚程序：systemctl stop $unit && cp -p '$backup/mirror-$role' '$binary' && systemctl start $unit"
