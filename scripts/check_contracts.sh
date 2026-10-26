#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
python3 scripts/generate.py --check
python3 scripts/test_codegen.py
(
  cd backend
  go test ./...
)
(
  cd frontend
  pnpm --filter @dexwallet/api-contract typecheck
)
