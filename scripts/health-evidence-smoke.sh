#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

go test ./backend/app -run 'HealthEvidence|KnowledgeContractHealthEvidence|KnowledgeContractSchemaFiles' -count=1
echo "health evidence smoke passed"
