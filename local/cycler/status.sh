#!/usr/bin/env bash
# Show cycler run status: process, enclaves, and recent log (webhook masked).
set -uo pipefail

cd "$(dirname "$0")"

PAT='go-build.*/cycler|go run \.'
pids=$(pgrep -f "$PAT" 2>/dev/null || true)
if [ -n "$pids" ]; then
  echo "cycler: RUNNING (pids: $(echo "$pids" | tr '\n' ' '))"
else
  echo "cycler: not running"
fi

echo
echo "enclaves:"
kurtosis enclave ls 2>/dev/null || echo "  (kurtosis unavailable)"

LOG="${CYCLER_LOG:-$HOME/cycler.log}"
echo
echo "recent log ($LOG):"
if [ -f "$LOG" ]; then
  tail -n 15 "$LOG" | sed -E 's#https://hooks\.slack\.com/services/[^[:space:]]*#<redacted>#g'
else
  echo "  (no log yet)"
fi
