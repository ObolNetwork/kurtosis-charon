#!/usr/bin/env bash
# Stop the dv-cycler and tear down the enclave that was in flight when it
# stopped (best-effort), so it doesn't linger consuming resources. The state
# file is preserved, so a later start.sh resumes at the same rotation position.
set -uo pipefail

cd "$(dirname "$0")"

PAT='go-build.*/dv-cycler|go run \.'
pids=$(pgrep -f "$PAT" 2>/dev/null || true)
if [ -z "$pids" ]; then
  echo "dv-cycler not running"
else
  echo "stopping dv-cycler (pids: $(echo "$pids" | tr '\n' ' '))"
  # SIGTERM to `go run` is forwarded to the compiled child; kill both patterns
  # to be safe.
  pkill -TERM -f 'go run \.' 2>/dev/null || true
  pkill -TERM -f 'go-build.*/dv-cycler' 2>/dev/null || true
  sleep 2
fi

# Best-effort teardown of the in-flight enclave named in the state file.
STATE="${CYCLER_STATE_PATH:-}"
if [ -z "$STATE" ]; then
  STATE=$(grep -E '^CYCLER_STATE_PATH=' .env 2>/dev/null | sed -E 's/^CYCLER_STATE_PATH=//' || true)
fi
if [ -n "${STATE:-}" ] && [ -f "$STATE" ]; then
  enc=$(grep -oE '"current_enclave"[[:space:]]*:[[:space:]]*"[^"]*"' "$STATE" \
        | sed -E 's/.*:[[:space:]]*"([^"]*)"/\1/' || true)
  if [ -n "${enc:-}" ]; then
    echo "tearing down in-flight enclave: $enc"
    kurtosis enclave rm -f "$enc" >/dev/null 2>&1 || true
  fi
fi
echo "done"
