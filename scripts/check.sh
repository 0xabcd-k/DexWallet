#!/usr/bin/env bash
# Thin local orchestration; existing Go/pnpm/Python commands remain usable.
set -euo pipefail
cd "$(dirname "$0")/.."
umask 077
scope=${1:-common}
case "$scope" in common|browser|integration|http|all) ;; *) echo 'Usage: bash scripts/check.sh [common|browser|integration|http|all]' >&2; exit 2;; esac
[[ $# -le 1 ]] || exit 2
mkdir -p .tools/check-runs
run_dir=$(mktemp -d "$PWD/.tools/check-runs/run-XXXXXXXX")
summary="$run_dir/summary.txt"
current=preflight
finish() {
  code=$?
  printf 'Result: scope=%s stage=%s exit=%s\n' "$scope" "$current" "$code" | tee -a "$summary"
  printf 'Evidence: %s\n' "$run_dir"
}
trap finish EXIT
printf 'Selected scope: %s\nUnselected/subsequent stages are not counted as passed.\n' "$scope" | tee "$summary"
# Check pinned tools without installing or upgrading them.
expected_go=$(python3 -c 'import json; print(json.load(open("scripts/tools.json"))["go"])')
actual_go=$(cd backend && go env GOVERSION)
[[ "$actual_go" == "go$expected_go" ]] || { echo "Use Go $expected_go" >&2; exit 1; }
if [[ "$scope" != integration ]]; then
  [[ $(node --version) == "v$(cat frontend/.nvmrc)" ]] || { echo 'Select Node from frontend/.nvmrc first.' >&2; exit 1; }
  expected_pnpm=$(node -p "JSON.parse(require('fs').readFileSync('frontend/package.json')).packageManager.split('@')[1]")
  [[ $(pnpm --version) == "$expected_pnpm" ]] || { echo "Use pnpm $expected_pnpm" >&2; exit 1; }
  [[ -d frontend/node_modules ]] || { echo 'Run pnpm install --frozen-lockfile in frontend/ first.' >&2; exit 1; }
fi
stage() {
  current=$1; shift
  local started=$SECONDS
  printf 'START %s\n' "$current" | tee -a "$summary"
  "$@" 2>&1 | tee "$run_dir/$current.log"
  printf 'PASS %s elapsed=%ss\n' "$current" "$((SECONDS-started))" | tee -a "$summary"
}
common() {
  python3 scripts/check_docs.py
  git diff --check
  python3 scripts/generate.py --check
  python3 scripts/test_codegen.py
  (cd backend && go test -race ./... && go vet ./...)
  (cd frontend && pnpm typecheck && pnpm test && pnpm build)
}
browser() { (cd frontend && pnpm build && pnpm test:e2e); }
integration() { (cd backend && go run ./tools/testenv run go test -tags=integration ./tests/integration -v -count=1 -timeout=180s); }
http() { (cd frontend && pnpm test:integration); }
if [[ "$scope" == all ]]; then
  for name in common browser integration http; do stage "$name" "$name"; done
else
  stage "$scope" "$scope"
fi
current=complete
