#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
count=${1:-1}
rate=${2:-100}
seconds=${3:-10}
[[ $count =~ ^(1|5|10|15)$ && $rate =~ ^[1-9][0-9]{0,5}$ && $seconds =~ ^[1-9][0-9]{0,3}$ ]] || {
  echo "Использование: $0 {1|5|10|15} строк_в_секунду_на_агента секунд" >&2
  exit 2
}
(( rate <= 100000 && seconds <= 3600 )) || exit 2
run="test-$(date -u +%Y%m%dT%H%M%S)-$$-${RANDOM}"
dc=(docker compose --profile lab --profile source)
formats=(json syslog nginx)
sources=(source-alpine source-debian source-ubuntu)
agents=()
for ((i=1; i<=count; i++)); do
  printf -v agent 'agent-%02d' "$i"
  agents+=("$agent")
done

sql() {
  "${dc[@]}" exec -T clickhouse clickhouse-client --password demo-local "$@"
}

if [[ ${SKIP_BUILD:-0} != 1 ]]; then
  "${dc[@]}" build clickhouse collector agent-01 "${sources[@]}"
fi
"${dc[@]}" up -d --wait collector
# Initialize the files first so the agents start at a known EOF.
for ((i=1; i<=count; i++)); do
  format=${formats[(i-1)%3]}
  source=${sources[(i-1)%3]}
  "${dc[@]}" run -T --rm --no-deps "$source" -index "$i" -source "$format" -init
done
"${dc[@]}" up -d --no-build --force-recreate "${agents[@]}"
for agent in "${agents[@]}"; do
  deadline=$((SECONDS+30))
  until "${dc[@]}" logs --no-color "$agent" | grep -F 'ready ' >/dev/null; do
    if (( SECONDS >= deadline )); then
      "${dc[@]}" logs --no-color "$agent" >&2
      exit 1
    fi
    sleep 1
  done
done

expected=$((count*rate*seconds))
started=$(date +%s%N)
pids=()
for ((i=1; i<=count; i++)); do
  format=${formats[(i-1)%3]}
  source=${sources[(i-1)%3]}
  "${dc[@]}" run -T --rm --no-deps "$source" -index "$i" -source "$format" \
    -seconds "$seconds" -rate "$rate" -run "$run" &
  pids+=("$!")
done
failed=0
for pid in "${pids[@]}"; do
  wait "$pid" || failed=1
done
if ((failed)); then
  echo "Ошибка генерации." >&2
  exit 1
fi

deadline=$((SECONDS+60))
stored=0
unique=0
query="SELECT count(), uniqExact(tuple(agent,message)) FROM logs WHERE position(message,{run:String})>0 FORMAT TSVRaw"
while :; do
  totals=$(sql --param_run="$run" --query "$query")
  IFS=$'\t' read -r stored unique <<< "$totals"
  (( unique >= expected || SECONDS >= deadline )) && break
  sleep 0.25
done
elapsed_ms=$(( ($(date +%s%N)-started)/1000000 ))
observed=$(awk -v n="$unique" -v ms="$elapsed_ms" 'BEGIN {printf "%.2f", n*1000/ms}')
{
  echo "run,agents,rate_per_agent,configured_seconds,expected,stored,unique,elapsed_ms,observed_unique_per_sec"
  echo "$run,$count,$rate,$seconds,$expected,$stored,$unique,$elapsed_ms,$observed"
}
echo "Интерфейс: http://localhost:8080/?text=$run"
(( stored==expected && unique==expected ))
