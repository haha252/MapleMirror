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
mkdir -p "$prefix/backups"
backup=$(mktemp -d "$prefix/backups/control-reconnect-$stamp-XXXXXX")
cp -p "$binary" "$backup/mirror-$role"
sha256sum "$backup/mirror-$role" > "$backup/old-binary.sha256"
systemctl show -p ExecStart -p FragmentPath "$unit" > "$backup/service.txt"
install -m 0755 "$bundle/mirror-$role" "$binary.control-reconnect-new"
replaced=0
recover_on_error() {
  result=$?
  trap - EXIT
  if [[ $result != 0 ]]; then
    if [[ $replaced == 1 ]]; then
      systemctl stop "$unit" || true
      cp -p "$backup/mirror-$role" "$binary.control-reconnect-rollback"
      mv -f "$binary.control-reconnect-rollback" "$binary"
    fi
    rm -f "$binary.control-reconnect-new"
    systemctl start "$unit" || true
    echo "安装失败，已尝试恢复旧程序；备份：$backup" >&2
  fi
  exit "$result"
}
trap recover_on_error EXIT
systemctl stop "$unit"
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
mv -f "$binary.control-reconnect-new" "$binary"
replaced=1
systemctl start "$unit"
systemctl is-active --quiet "$unit"
trap - EXIT
echo "安装完成，备份目录：$backup"
echo "仍需观察报告、积压与实际下载；is-active 只表示进程启动。"
echo "查看日志：journalctl -u $unit --since '5 minutes ago' --no-pager -n 100"
echo "回滚程序：systemctl stop $unit && cp -p '$backup/mirror-$role' '$binary' && systemctl start $unit"
