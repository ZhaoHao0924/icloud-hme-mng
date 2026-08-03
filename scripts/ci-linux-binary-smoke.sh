#!/usr/bin/env bash

set -euo pipefail

binary="${1:?usage: ci-linux-binary-smoke.sh <binary> [port]}"
port="${2:-18082}"
listen_addr="127.0.0.1:${port}"
base_url="http://${listen_addr}"
data_dir="$(mktemp -d)"
server_log="$(mktemp)"
root_headers="$(mktemp)"
asset_headers="$(mktemp)"
root_body="$(mktemp)"
unauthenticated_health_body="$(mktemp)"
invalid_token_health_body="$(mktemp)"
api_token="$(openssl rand -hex 24)"
server_pid=""

on_error() {
  local status=$?
  printf 'ci-linux-binary-smoke failed at line %s (exit %s)\n' "${1:-unknown}" "$status" >&2
  if [[ -s "$server_log" ]]; then
    cat "$server_log" >&2
  fi
  exit "$status"
}

cleanup() {
  local status=$?
  trap - EXIT

  if [[ -n "$server_pid" ]]; then
    if kill -0 "$server_pid" 2>/dev/null; then
      kill "$server_pid" 2>/dev/null || true
      for _ in $(seq 1 10); do
        if ! kill -0 "$server_pid" 2>/dev/null; then
          break
        fi
        sleep 1
      done
      kill -9 "$server_pid" 2>/dev/null || true
    fi
    wait "$server_pid" 2>/dev/null || true
  fi

  rm -rf "$data_dir" "$server_log" "$root_headers" "$asset_headers" \
    "$root_body" "$unauthenticated_health_body" "$invalid_token_health_body"
  exit "$status"
}

wait_for_server() {
  for _ in $(seq 1 30); do
    if ! kill -0 "$server_pid" 2>/dev/null; then
      cat "$server_log" >&2 || true
      return 1
    fi
    if curl --silent --show-error --fail \
      --header "Authorization: Bearer $api_token" \
      "$base_url/api/health" >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done

  cat "$server_log" >&2 || true
  return 1
}

trap 'on_error "$LINENO"' ERR
trap cleanup EXIT

test -x "$binary"
printf '::add-mask::%s\n' "$api_token"

ICLOUD_HME_API_TOKEN="$api_token" "$binary" \
  -addr "$listen_addr" \
  -data "$data_dir" \
  >"$server_log" 2>&1 &
server_pid=$!
wait_for_server

test "$(curl --silent --show-error --output "$unauthenticated_health_body" \
  --write-out '%{http_code}' "$base_url/api/health")" = "401"
jq --exit-status '.success == false and .code == "platform_auth_setup_required"' \
  "$unauthenticated_health_body" >/dev/null

test "$(curl --silent --show-error --output "$invalid_token_health_body" \
  --write-out '%{http_code}' \
  --header "Authorization: Bearer ${api_token}.invalid" \
  "$base_url/api/health")" = "401"
jq --exit-status '.success == false and .code == "api_token_invalid"' \
  "$invalid_token_health_body" >/dev/null

curl --silent --show-error --fail \
  --header "Accept: text/html" \
  --dump-header "$root_headers" \
  --output "$root_body" \
  "$base_url/"
grep --ignore-case --quiet '^cache-control: no-cache' "$root_headers"
grep --ignore-case --quiet '^content-security-policy: .*frame-ancestors' "$root_headers"
grep --ignore-case --quiet '^x-content-type-options: nosniff' "$root_headers"

asset_path="$(sed -nE 's#.*(src|href)="([^"]*/assets/[^"]+)".*#\2#p' \
  "$root_body" | head -n 1)"
test -n "$asset_path"
curl --silent --show-error --fail \
  --dump-header "$asset_headers" \
  --output /dev/null \
  "$base_url$asset_path"
grep --ignore-case --quiet '^cache-control: public, max-age=31536000, immutable' \
  "$asset_headers"

curl --silent --show-error --fail \
  --header "Accept: text/html" \
  "$base_url/accounts/ci-smoke/aliases" | grep --quiet '<div id="root"></div>'
test "$(curl --silent --output /dev/null --write-out '%{http_code}' \
  "$base_url/assets/missing.js")" = "404"
test "$(curl --silent --output /dev/null --write-out '%{http_code}' \
  --header "Authorization: Bearer $api_token" \
  "$base_url/api/missing")" = "404"

curl --silent --show-error --fail \
  --header "Authorization: Bearer $api_token" \
  "$base_url/api/health" | jq --exit-status \
  '.success == true and .data.service == "icloud-hme" and .data.version == "ci"' >/dev/null

printf 'Linux binary smoke passed\n'
