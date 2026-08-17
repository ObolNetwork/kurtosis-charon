#!/usr/bin/env bash
# Stop the cycler: stop the loop process AND tear down the in-flight enclave
# (the combo running right now). The state file is preserved, so a later
# start.sh resumes at the same rotation position.
set -uo pipefail

cd "$(dirname "$0")"

PAT='go-build.*/cycler|go run \.'
pids=$(pgrep -f "$PAT" 2>/dev/null || true)
if [ -z "$pids" ]; then
  echo "cycler not running"
else
  echo "stopping cycler (pids: $(echo "$pids" | tr '\n' ' '))"
  # SIGTERM to `go run` is forwarded to the compiled child; kill both to be safe.
  pkill -TERM -f 'go run \.' 2>/dev/null || true
  pkill -TERM -f 'go-build.*/cycler' 2>/dev/null || true
  sleep 2
fi

# Tear down the in-flight enclave named in the state file (best-effort).
STATE="${CYCLER_STATE_PATH:-}"
if [ -z "$STATE" ]; then
  STATE=$(grep -E '^CYCLER_STATE_PATH=' .env 2>/dev/null | sed -E 's/^CYCLER_STATE_PATH=//' || true)
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
