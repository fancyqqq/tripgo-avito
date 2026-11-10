#!/usr/bin/env bash
set -u

BASE_URL="${BASE_URL:-http://localhost:8080}"
DURATION="${DURATION:-180}"
WORKERS="${WORKERS:-4}"
PAUSE="${PAUSE:-0.2}"

API="$BASE_URL/api/v1/trips"

uuid() {
  cat /proc/sys/kernel/random/uuid
}

body() {
  local driver="$1"
  printf '{"user_id":"%s","driver_id":"%s","start_point":{"latitude":55.75,"longitude":37.61},"end_point":{"latitude":55.80,"longitude":37.70},"price":%d}' \
    "$(uuid)" "$driver" $((RANDOM % 2000))
}

create() {
  local response
  response=$(curl -s -w '\n%{http_code}' -X POST "$API" -H 'Content-Type: application/json' -d "$(body "$1")")
  echo "${response##*$'\n'}" >>"$results"
  sed -nE 's/.*"id":"([^"]+)".*/\1/p' <<<"${response%$'\n'*}"
}

call() {
  curl -s -o /dev/null -w '%{http_code}\n' "$@"
}

happy_path() {
  local id
  id=$(create "$(uuid)")
  [ -z "$id" ] && { echo 000; return; }
  call "$API/$id"
  call -X POST "$API/$id/finish"
}

double_finish() {
  local id
  id=$(create "$(uuid)")
  [ -z "$id" ] && { echo 000; return; }
  call -X POST "$API/$id/finish"
  call -X POST "$API/$id/finish"
}

driver_busy() {
  local driver id
  driver=$(uuid)
  id=$(create "$driver")
  call -X POST "$API" -H 'Content-Type: application/json' -d "$(body "$driver")"
  [ -n "$id" ] && call -X POST "$API/$id/finish"
}

not_found() {
  call "$API/$(uuid)"
}

bad_id() {
  call "$API/not-a-uuid"
}

bad_body() {
  call -X POST "$API" -H 'Content-Type: application/json' -d '{"user_id":"oops"}'
}

worker() {
  local end=$((SECONDS + DURATION)) roll
  while [ "$SECONDS" -lt "$end" ]; do
    roll=$((RANDOM % 100))
    if   [ "$roll" -lt 55 ]; then happy_path
    elif [ "$roll" -lt 70 ]; then double_finish
    elif [ "$roll" -lt 80 ]; then driver_busy
    elif [ "$roll" -lt 88 ]; then not_found
    elif [ "$roll" -lt 94 ]; then bad_id
    else bad_body
    fi
    sleep "$PAUSE"
  done
}

if ! curl -s -o /dev/null "$BASE_URL/health"; then
  echo "service is not reachable at $BASE_URL, start it with make run" >&2
  exit 1
fi

results=$(mktemp)
trap 'rm -f "$results"' EXIT

echo "load: $WORKERS workers, ${DURATION}s, target $BASE_URL"

for _ in $(seq 1 "$WORKERS"); do
  worker >>"$results" &
done
wait

echo "responses by status:"
sort "$results" | uniq -c | sort -rn
