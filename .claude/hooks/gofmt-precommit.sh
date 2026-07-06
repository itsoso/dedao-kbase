#!/usr/bin/env bash
# PreToolUse(Bash)hook —— git commit 前用 gofmt 拦未格式化的 Go(AGENTS.md:Format Go with gofmt)。
# 快(只列文件)、确定性、本地阻断。SKIP_GOFMT_HOOK=1 一次性绕过;gofmt 缺失 fail-open。
set -uo pipefail
input="$(cat)"
printf '%s' "$input" | grep -q "git commit" || exit 0           # 无子串→零开销放行
[ "${SKIP_GOFMT_HOOK:-}" = "1" ] && exit 0
cmd="$(printf '%s' "$input" | python3 -c "import sys,json;print((json.load(sys.stdin).get('tool_input') or {}).get('command',''))" 2>/dev/null || printf '%s' "$input")"
# 命令边界精确匹配:只认真正在调 git commit(排除 echo/grep 里的字符串、git log --grep)
printf '%s' "$cmd" | grep -qE '(^|[;&|(])[[:space:]]*git[[:space:]]+commit([[:space:]]|$)' || exit 0

root="${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null)}"
[ -n "$root" ] && cd "$root" 2>/dev/null || exit 0

if command -v gofmt >/dev/null 2>&1; then
  unformatted="$(gofmt -l backend/ cmd/ 2>/dev/null)"
  if [ -n "$unformatted" ]; then
    {
      echo "🚫 gofmt 闸拦截 git commit —— 以下 Go 文件未格式化:"
      printf '%s\n' "$unformatted"
      echo "修:gofmt -w <上述文件>(或 gofmt -w backend/ cmd/),再提交。"
      echo "临时绕过(仅本次): SKIP_GOFMT_HOOK=1 <你的 git commit 命令>"
    } >&2
    exit 2
  fi
fi

if [ -x scripts/privacy-smoke.sh ]; then
  if ! bash scripts/privacy-smoke.sh; then
    {
      echo "privacy-smoke 闸拦截 git commit。"
      echo "修复隐私泄漏或跟踪的本地产物后再提交。"
    } >&2
    exit 2
  fi
fi
