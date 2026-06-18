#!/usr/bin/env sh
set -eu

usage() {
  cat <<'EOF'
用法:
  scripts/count-lines.sh [--root PATH] [--include-docs] [--include-meta] [--top N] [--max-per-file N] [--quiet]

说明:
  默认统计 Git 跟踪的源码和常见文本资源文件，排除图片、SVG、docs/ 里的文档，
  以及 go.mod / go.sum / .gitignore / .gitattributes / sponsor.json 这类元数据。

选项:
  --root PATH        指定仓库根目录，默认自动从当前脚本位置向上查找 Git 根目录
  --include-docs     把 docs/ 下的 Markdown 文档也算进去
  --include-meta     把 go.mod、go.sum、.gitignore、.gitattributes 和 sponsor.json 也算进去
  --top N            额外显示行数最多的前 N 个文件
  --max-per-file N   如果某个文件行数超过 N，则退出失败
  --quiet            只在失败时输出超限文件，不打印汇总
  -h, --help        显示帮助
EOF
}

ROOT=
INCLUDE_DOCS=0
INCLUDE_META=0
TOP_N=0
MAX_PER_FILE=
QUIET=0

while [ $# -gt 0 ]; do
  case "$1" in
    --root)
      [ $# -ge 2 ] || { printf '%s\n' "--root 需要一个路径参数" >&2; exit 2; }
      ROOT=$2
      shift 2
      ;;
    --include-docs)
      INCLUDE_DOCS=1
      shift
      ;;
    --include-meta)
      INCLUDE_META=1
      shift
      ;;
    --top)
      [ $# -ge 2 ] || { printf '%s\n' "--top 需要一个数字参数" >&2; exit 2; }
      TOP_N=$2
      shift 2
      ;;
    --max-per-file)
      [ $# -ge 2 ] || { printf '%s\n' "--max-per-file 需要一个数字参数" >&2; exit 2; }
      MAX_PER_FILE=$2
      shift 2
      ;;
    --quiet)
      QUIET=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf '%s\n' "未知参数: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ -z "$ROOT" ]; then
  ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
else
  ROOT=$(CDPATH= cd -- "$ROOT" && pwd)
fi

TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/mirror-server-count-lines.XXXXXX")
trap 'rm -rf "$TMPDIR"' EXIT INT HUP TERM

FILES="$TMPDIR/files.txt"
DATA="$TMPDIR/data.tsv"
TMP_OVERS="$TMPDIR/overs.tsv"
printf '' > "$DATA"
printf '' > "$TMP_OVERS"
git -C "$ROOT" ls-files > "$FILES"

should_include() {
  file=$1

  case "$file" in
    */.git/*|.gitignore|.gitattributes)
      [ "$INCLUDE_META" -eq 1 ] || return 1
      return 0
      ;;
    docs/*)
      [ "$INCLUDE_DOCS" -eq 1 ] || return 1
      case "$file" in
        *.md|*.markdown|*.txt) return 0 ;;
        *) return 1 ;;
      esac
      ;;
    go.mod|go.sum|sponsor.json|configs/sponsors.json)
      [ "$INCLUDE_META" -eq 1 ] || return 1
      return 0
      ;;
    *.png|*.jpg|*.jpeg|*.webp|*.gif|*.ico|*.bmp|*.pdf)
      return 1
      ;;
    *.svg)
      return 1
      ;;
  esac

  case "$file" in
    *.go|*.js|*.css|*.html|*.sh|*.c|*.yaml|*.yml|*.sql|*.ps1|*.bat|*.md|*.markdown|*.txt)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

ext_of() {
  file=$1
  base=${file##*/}
  case "$base" in
    .*)
      printf '%s' "[noext]"
      return
      ;;
  esac
  case "$base" in
    *.*)
      printf '%s' "${base##*.}" | tr '[:upper:]' '[:lower:]'
      ;;
    *)
      printf '%s' "[noext]"
      ;;
  esac
}

files_count=0
lines_count=0

while IFS= read -r file; do
  [ -n "$file" ] || continue
  should_include "$file" || continue
  file_path=$ROOT/$file
  [ -f "$file_path" ] || continue
  lines=$(wc -l < "$file_path")
  ext=$(ext_of "$file")
  printf '%s\t%s\t%s\n' "$lines" "$ext" "$file" >> "$DATA"
  if [ -n "$MAX_PER_FILE" ] && [ "$lines" -gt "$MAX_PER_FILE" ]; then
    printf '%s\t%s\n' "$lines" "$file" >> "$TMP_OVERS"
  fi
  files_count=$((files_count + 1))
  lines_count=$((lines_count + lines))
done < "$FILES"

if [ "$QUIET" -eq 0 ]; then
  printf '仓库: %s\n' "$ROOT"
  printf '统计文件数: %s\n' "$files_count"
  printf '总行数: %s\n' "$lines_count"
  printf '\n'
  printf '%s\n' "按扩展名统计:"
  awk -F '\t' '
    {
      lines[$2] += $1
      files[$2] += 1
    }
    END {
      for (ext in lines) {
        printf "%s\t%d\t%d\n", ext, lines[ext], files[ext]
      }
    }
  ' "$DATA" | sort -k2,2nr | awk -F '\t' '{ printf "  %-10s %8d 行  (%d 个文件)\n", $1, $2, $3 }'

  if [ "$TOP_N" -gt 0 ]; then
    printf '\n'
    printf '%s\n' "行数最多的前 $TOP_N 个文件:"
    sort -t "$(printf '\t')" -k1,1nr "$DATA" | awk -F '\t' -v n="$TOP_N" '
      NR <= n {
        printf "  %s 行  %s\n", $1, $3
      }
    '
  fi
fi

if [ -n "$MAX_PER_FILE" ] && [ -s "$TMP_OVERS" ]; then
  printf '\n'
  printf '%s\n' "以下文件超过 ${MAX_PER_FILE} 行："
  sort -t "$(printf '\t')" -k1,1nr "$TMP_OVERS" | awk -F '\t' '{
    printf "  %s 行  %s\n", $1, $2
  }'
  exit 1
fi
