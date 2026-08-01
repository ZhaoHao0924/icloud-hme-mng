#!/usr/bin/env bash

set -euo pipefail

image="${1:?usage: ci-docker-smoke.sh <image>}"
container_name="icloud-hme-ci-${RANDOM}-${RANDOM}"
data_dir="$(mktemp -d)"
root_headers="$(mktemp)"
asset_headers="$(mktemp)"
root_body="$(mktemp)"
unauthenticated_health_body="$(mktemp)"
invalid_token_health_body="$(mktemp)"
api_token="$(openssl rand -hex 24)"
base_url="http://127.0.0.1:18081"

cleanup() {
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  rm -rf "$data_dir" "$root_headers" "$asset_headers" "$root_body" "$unauthenticated_health_body" "$invalid_token_health_body"
}

wait_for_server() {
  for _ in $(seq 1 30); do
    if curl --silent --show-error --fail \
      --header "Authorization: Bearer $api_token" \
      "$base_url/api/health" >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done

  docker logs "$container_name" >&2 || true
  return 1
}

start_container() {
  docker run --detach --rm \
    --name "$container_name" \
    --publish "127.0.0.1:18081:8081" \
    --env "ICLOUD_HME_API_TOKEN=$api_token" \
    --volume "$data_dir:/app/data" \
    "$image" \
    -addr 0.0.0.0:8081 >/dev/null
  wait_for_server
}

stop_container() {
  docker stop "$container_name" >/dev/null
  for _ in $(seq 1 10); do
    if ! docker container inspect "$container_name" >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done
  return 1
}

trap cleanup EXIT
printf '::add-mask::%s\n' "$api_token"

start_container

test "$(curl --silent --show-error --output "$unauthenticated_health_body" --write-out '%{http_code}' "$base_url/api/health")" = "401"
jq --exit-status '.success == false and .code == "api_token_invalid"' "$unauthenticated_health_body" >/dev/null
test "$(curl --silent --show-error --output "$invalid_token_health_body" --write-out '%{http_code}' --header "Authorization: Bearer ${api_token}.invalid" "$base_url/api/health")" = "401"
jq --exit-status '.success == false and .code == "api_token_invalid"' "$invalid_token_health_body" >/dev/null

curl --silent --show-error --fail \
  --header "Accept: text/html" \
  --dump-header "$root_headers" \
  --output "$root_body" \
  "$base_url/"
grep --ignore-case --quiet '^cache-control: no-cache' "$root_headers"
grep --ignore-case --quiet '^content-security-policy: .*frame-ancestors' "$root_headers"
grep --ignore-case --quiet '^x-content-type-options: nosniff' "$root_headers"

asset_path="$(sed -nE 's#.*(src|href)="([^"]*/assets/[^"]+)".*#\2#p' "$root_body" | head -n 1)"
test -n "$asset_path"
curl --silent --show-error --fail \
  --dump-header "$asset_headers" \
  --output /dev/null \
  "$base_url$asset_path"
grep --ignore-case --quiet '^cache-control: public, max-age=31536000, immutable' "$asset_headers"

curl --silent --show-error --fail \
  --header "Accept: text/html" \
  "$base_url/accounts/ci-smoke/aliases" | grep --quiet '<div id="root"></div>'
test "$(curl --silent --output /dev/null --write-out '%{http_code}' "$base_url/assets/missing.js")" = "404"
test "$(curl --silent --output /dev/null --write-out '%{http_code}' "$base_url/assets/ci-stale-sentinel.js")" = "404"
test "$(curl --silent --output /dev/null --write-out '%{http_code}' \
  --header "Authorization: Bearer $api_token" \
  "$base_url/api/missing")" = "404"

curl --silent --show-error --fail \
  --header "Authorization: Bearer $api_token" \
  "$base_url/api/health" | jq --exit-status \
  '.success == true and .data.service == "icloud-hme" and .data.version == "ci"' >/dev/null

curl --silent --show-error --fail \
  --request POST \
  --header "Authorization: Bearer $api_token" \
  --header "Content-Type: application/json" \
  --data '{"name":"CI smoke account","icloud_email":"ci-smoke@icloud.com","host":"icloud.com"}' \
  "$base_url/api/accounts" | jq --exit-status \
  '.success == true and .data.name == "CI smoke account"' >/dev/null

stop_container
start_container

curl --silent --show-error --fail \
  --header "Authorization: Bearer $api_token" \
  "$base_url/api/accounts" | jq --exit-status \
  '.success == true and (.data | length == 1) and .data[0].icloud_email == "ci-smoke@icloud.com"' >/dev/null
