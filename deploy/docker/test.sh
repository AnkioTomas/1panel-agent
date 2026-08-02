#!/bin/sh
set -eu
cd "$(dirname "$0")"

echo "==> build & up"
docker compose up -d --build --force-recreate

cleanup() {
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> wait agent online"
ok=0
for i in $(seq 1 60); do
  body=$(curl -fsS http://127.0.0.1:18080/ || true)
  if echo "$body" | grep -q 'id='; then
    ok=1
    break
  fi
  sleep 1
done
if [ "$ok" != 1 ]; then
  echo "agent never appeared on master"
  docker compose logs
  exit 1
fi
echo "$body" | head -n 40

agent_id=$(echo "$body" | sed -n 's/.*id=\([0-9a-f]*\).*/\1/p' | head -n 1)
if [ -z "$agent_id" ]; then
  echo "failed to parse agent id"
  exit 1
fi
echo "==> agent_id=$agent_id"

echo "==> proxy /hello"
headers=$(mktemp)
body=$(curl -fsS -D "$headers" "http://127.0.0.1:18080/n/${agent_id}/hello")
echo "body=$body"
grep -i 'X-Echo-Token:' "$headers"
grep -i 'Path=/n/'"$agent_id"'/' "$headers"

if [ "$body" != "panel-ok" ]; then
  echo "unexpected body"
  exit 1
fi
if ! grep -qi 'X-Echo-Token: .' "$headers"; then
  echo "missing injected 1Panel-Token"
  cat "$headers"
  exit 1
fi

echo "==> PASS"
