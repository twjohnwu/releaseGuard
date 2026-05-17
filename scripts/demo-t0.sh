#!/bin/bash
# Topology 0 端對端 demo——跑 analyzer 並印出最新 MR comment 供截圖
set -e
cd "$(dirname "$0")/.."

export POSTGRES_URL="postgresql://postgres:postgres@localhost:5432/releaseguard_dev"
export GITLAB_TOKEN="fake"
export GITLAB_API_BASE="http://localhost:8080/api/v4"
export AI_PROVIDER_KEY="fake"
export RG_AGENT_AI_REVIEWER_ENABLED="false"
export RG_AGENT_OWNERSHIP_ENABLED="true"
export RG_AGENT_SELECTIVE_TEST_ENABLED="true"
export RG_AGENT_ROLLOUT_RISK_ENABLED="true"
export RG_RAG_ENABLED="false"
export TARGET_SERVICE_NAME="releaseGuard"
export TARGET_SERVICE_TYPE="backend"
export SELECTIVE_TEST_MIN_CONFIDENCE="0.5"
export ANALYZE_TIMEOUT_SEC="60"
export CI_PROJECT_ID="4"
export CI_MERGE_REQUEST_IID="4"
export CI_COMMIT_SHA="t0-l3-demo"

./bin/analyzer

echo
echo "=== latest MR comment (paste into GitHub issue/gist for screenshot) ==="
ls -t deploy/compose/artifacts/note-proj4-*.md | head -1 | xargs cat
