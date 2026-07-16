#!/usr/bin/env bash
set -euo pipefail

go test ./backend/app -run 'KnowledgeContract|KnowledgeFeed|DeliveryReceipt|KnowledgeLineage|KnowledgeImpact|KnowledgeGaps' -v
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
