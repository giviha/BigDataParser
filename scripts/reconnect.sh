#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
dc=(docker compose --profile lab --profile source)
run="reconnect-$(date -u +%Y%m%dT%H%M%S)-$$"
"${dc[@]}" run -T --rm --no-deps source-alpine -index 1 -source json -init
"${dc[@]}" up -d --no-build --force-recreate agent-01
container=$("${dc[@]}" ps -q agent-01)
network=$(docker inspect --format '{{range $name, $info := .NetworkSettings.Networks}}{{$name}}{{end}}' "$container")
deadline=$((SECONDS+30))
until "${dc[@]}" logs --no-color agent-01 | grep -F 'ready ' >/dev/null; do
  (( SECONDS < deadline )) || exit 1
  sleep 1
done
count_records() {
  "${dc[@]}" exec -T clickhouse clickhouse-client --password demo-local \
    --param_run="$run" --query "SELECT count(), uniqExact(message) FROM logs WHERE agent='agent-01' AND position(message,{run:String})>0 FORMAT TSVRaw"
}
restore_network() { docker network connect "$network" "$container"; }
docker network disconnect "$network" "$container"
trap 'restore_network || true' EXIT
"${dc[@]}" run -T --rm --no-deps source-alpine -index 1 -source json \
  -rate 100 -seconds 6 -run "$run"
sleep 12
offline=$(count_records)
restore_network
trap - EXIT

deadline=$((SECONDS+60))
while :; do
  totals=$(count_records)
  IFS=$'\t' read -r stored unique <<< "$totals"
  (( unique >= 600 || SECONDS >= deadline )) && break
  sleep 1
done
{
  printf 'run,expected,stored_offline,stored_after_reconnect,unique_after_reconnect\n'
  printf '%s,600,%s,%d,%d\n' "$run" "${offline%%$'\t'*}" "$stored" "$unique"
}
[[ $offline == $'0\t0' && $stored == 600 && $unique == 600 ]]
