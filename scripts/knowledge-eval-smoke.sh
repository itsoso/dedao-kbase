#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

go test ./backend/app -run KnowledgeEval -count=1

echo "knowledge eval smoke passed"
