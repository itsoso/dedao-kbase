#!/usr/bin/env bash
set -euo pipefail

go test ./backend/app -run 'KnowledgeContract|KnowledgeFeed|DeliveryReceipt|KnowledgeLineage|KnowledgeImpact|KnowledgeGaps' -v
bash scripts/proof-consumer-contract-smoke.sh
bash scripts/health-evidence-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
