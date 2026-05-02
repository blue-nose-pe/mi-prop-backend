#!/usr/bin/env bash
# Equivalent to smoke-test.ps1 — el pipeline (Linux) lo usa.
#
# Uso: ./smoke-test.sh https://api.miproposito.ucsp.edu.pe
set -euo pipefail
BASE="${1:?BaseURL required}"
EMAIL="${SMOKE_ADMIN_EMAIL:-}"
PASS="${SMOKE_ADMIN_PASSWORD:-}"

step() { echo "→ $1"; }
fail() { echo "  ✗ FAIL: $1" >&2; exit 1; }
ok()   { echo "  ✓ ok"; }

step "GET /health"
status=$(curl -s "${BASE}/health" | grep -o '"status":"[^"]*"' || true)
[[ "$status" == '"status":"ok"' ]] || fail "health did not return ok ($status)"
ok

if [[ -z "$EMAIL" || -z "$PASS" ]]; then
  echo "Auth checks skipped (set SMOKE_ADMIN_EMAIL/SMOKE_ADMIN_PASSWORD to enable)"
  exit 0
fi

step "POST /api/auth/login"
login=$(curl -s -X POST "${BASE}/api/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"${EMAIL}\",\"password\":\"${PASS}\"}")
access=$(echo "$login"  | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
refresh=$(echo "$login" | sed -n 's/.*"refresh_token":"\([^"]*\)".*/\1/p')
[[ -n "$access" && -n "$refresh" ]] || fail "no tokens in login response: $login"
ok

step "POST /api/auth/refresh"
ref=$(curl -s -X POST "${BASE}/api/auth/refresh" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"${refresh}\"}")
new_refresh=$(echo "$ref" | sed -n 's/.*"refresh_token":"\([^"]*\)".*/\1/p')
[[ -n "$new_refresh" ]] || fail "no refresh_token after rotation: $ref"
ok

step "POST /api/auth/logout"
curl -fsS -X POST "${BASE}/api/auth/logout" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\":\"${new_refresh}\"}" >/dev/null
ok

echo
echo "All smoke checks passed ✓"
