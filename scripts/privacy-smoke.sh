#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

failed=0

if tracked_ds_store="$(git ls-files '*DS_Store')" && [[ -n "${tracked_ds_store}" ]]; then
  echo "Tracked .DS_Store files are not allowed:"
  echo "${tracked_ds_store}"
  failed=1
fi

check_pattern() {
  local label="$1"
  local pattern="$2"
  if matches="$(git grep -n -I -E -e "${pattern}" -- . ':!scripts/privacy-smoke.sh')" && [[ -n "${matches}" ]]; then
    echo "Forbidden private pattern found: ${label}"
    echo "${matches}"
    failed=1
  fi
}

check_pattern "macOS absolute home path" '/Users/[[:alnum:]_.-]+/'
check_pattern "project-like llm-driven private name" '[[:alnum:]_.-]+-llm-driven'
check_pattern "project-like macd-analysis private name" 'macd-analysis-[[:alnum:]_.-]+'
check_pattern "AWS access key shape" 'AKIA[0-9A-Z]{16}'
check_pattern "Google API key shape" 'AIza[0-9A-Za-z_-]{35}'
check_pattern "GitHub token shape" 'gh[pousr]_[0-9A-Za-z_]{30,}'
check_pattern "OpenAI-like sk token shape" 'sk-[A-Za-z0-9]{20,}'
check_pattern "Slack token shape" 'xox[baprs]-[0-9A-Za-z-]{20,}'
check_pattern "JWT-like token shape" 'eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}'
check_pattern "private key header" '-----BEGIN [A-Z ]*PRIVATE KEY-----'
check_pattern "long sensitive assignment" '(api[_-]?key|secret|token|cookie|password)[[:space:]]*[:=][[:space:]]*["'\''`]?([A-Za-z0-9_./+=-]{32,})'

if [[ -n "${DEDAO_PRIVACY_EXTRA_GREP_PATTERNS:-}" ]]; then
  while IFS= read -r pattern; do
    [[ -z "${pattern}" || "${pattern}" =~ ^[[:space:]]*# ]] && continue
    check_pattern "extra denylist pattern" "${pattern}"
  done <<< "${DEDAO_PRIVACY_EXTRA_GREP_PATTERNS}"
fi

if [[ -f ".privacy-denylist.local" ]]; then
  while IFS= read -r pattern; do
    [[ -z "${pattern}" || "${pattern}" =~ ^[[:space:]]*# ]] && continue
    check_pattern "local denylist pattern" "${pattern}"
  done < ".privacy-denylist.local"
fi

if [[ "${failed}" -ne 0 ]]; then
  exit 1
fi

echo "privacy smoke passed"
