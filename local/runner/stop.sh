#!/usr/bin/env bash
# Stop the runner: stop the loop process AND tear down the in-flight enclave
# (the combo running right now). The state file is preserved, so a later
# start.sh resumes at the same rotation position.
set -uo pipefail

cd "$(dirname "$0")"

PAT='go-build.*/runner|go run \.|kurtosis-charon-runner'
pids=$(pgrep -f "$PAT" 2>/dev/null || true)
if [ -z "$pids" ]; then
  echo "runner not running"
else
  echo "stopping runner (pids: $(echo "$pids" | tr '\n' ' '))"
  # Kill everything the detection pattern matches: the `go run` supervisor,
  # its compiled child, and the staged self-restart binary the runner may
  # have exec'd into.
  pkill -TERM -f "$PAT" 2>/dev/null || true
  sleep 2
fi

# Tear down the in-flight enclave named in the state file (best-effort).
STATE="${RUNNER_STATE_PATH:-}"
if [ -z "$STATE" ]; then
  STATE=$(grep -E '^RUNNER_STATE_PATH=' .env 2>/dev/null | sed -E 's/^RUNNER_STATE_PATH=//' || true)
fi
enc=""
if [ -n "${STATE:-}" ] && [ -f "$STATE" ]; then
  enc=$(grep -oE '"current_enclave"[[:space:]]*:[[:space:]]*"[^"]*"' "$STATE" \
        | sed -E 's/.*:[[:space:]]*"([^"]*)"/\1/' || true)
fi

if [ -n "${enc:-}" ]; then
  echo "tearing down in-flight enclave: $enc"
  kurtosis enclave rm -f "$enc" >/dev/null 2>&1 && echo "removed $enc" \
    || echo "warning: could not remove $enc (already gone?)"
else
  echo "no in-flight enclave recorded"
fi
echo "done"
