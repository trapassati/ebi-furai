#!/usr/bin/env bash
# End-to-end tests: builds the server, starts it on a temporary data
# directory (your data is never touched), fills it with test feeds and runs
# the Playwright tests in a headless Chromium.
#
# Needs: Go, Python 3 with Playwright (pip install playwright, then
# "playwright install chromium", or CHROME=/path/to/chrome).
#
#   tests/e2e/run.sh                 all tests
#   tests/e2e/run.sh test_totp.py    only some
#
# READER_BIN=/path/to/ebi-furai uses an already built server instead.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../.." && pwd)"
port="${E2E_PORT:-9199}"
tmp="$(mktemp -d)"
trap 'kill "${pid:-0}" 2>/dev/null || true; rm -rf "$tmp"' EXIT

bin="${READER_BIN:-}"
if [[ -z "$bin" ]]; then
  echo "build…"
  (cd "$root" && sh scripts/fetch-gsap.sh && CGO_ENABLED=0 go build -o "$tmp/ebi-furai" ./cmd/ebi-furai)
  bin="$tmp/ebi-furai"
fi

export DATA_DIR="$tmp/data" ADMIN_USER=admin ADMIN_PASSWORD=e2e-password-123 COOKIE_SECURE=false \
  TRUST_CF_HEADER=false LISTEN_ADDR="127.0.0.1:$port" BACKUPS=true UPDATE_CHECK=false \
  PUBLIC_URL="http://127.0.0.1:$port"
mkdir -p "$DATA_DIR"
"$bin" >"$tmp/server.log" 2>&1 &
pid=$!
for _ in $(seq 50); do
  curl -sf "http://127.0.0.1:$port/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -sf "http://127.0.0.1:$port/healthz" >/dev/null || { cat "$tmp/server.log"; exit 1; }

export E2E_URL="http://127.0.0.1:$port" E2E_DB="$DATA_DIR/reader.db"
python3 "$here/seed.py" "$E2E_DB"

tests=("$@")
[[ ${#tests[@]} -eq 0 ]] && tests=($(cd "$here" && ls test_*.py))
failed=0
for t in "${tests[@]}"; do
  echo "== $t"
  python3 "$here/$t" || failed=$((failed + 1))
done
if [[ $failed -gt 0 ]]; then
  echo "server log:"; tail -n 30 "$tmp/server.log"
  echo "$failed file(s) with failures"
  exit 1
fi
echo "all passed"
