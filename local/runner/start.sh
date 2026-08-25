#!/usr/bin/env bash
# Start the runner as a detached background process that survives logout.
# Idempotent: refuses to start a second instance. Config is read from .env in
# this directory (see .env.example). Override the log path with RUNNER_LOG.
set -euo pipefail

cd "$(dirname "$0")"

# The runner runs via `go run .`, so a Go toolchain must be on PATH. Fall back
# to common install locations if `go` isn't already resolvable.
if ! command -v go >/dev/null 2>&1; then
  for d in "$HOME/sdk/go/bin" /usr/local/go/bin; do
    if [ -x "$d/go" ]; then
      export PATH="$d:$PATH"
      break
    fi
  done
fi
if ! command -v go >/dev/null 2>&1; then
  echo "error: go toolchain not found on PATH" >&2
  exit 1
fi

LOG="${RUNNER_LOG:-$HOME/runner.log}"
PAT='go-build.*/runner|go run \.|kurtosis-charon-runner'

if pgrep -f "$PAT" >/dev/null 2>&1; then
  echo "runner already running (pids: $(pgrep -f "$PAT" | tr '\n' ' '))"
  exit 0
fi

setsid nohup go run . > "$LOG" 2>&1 < /dev/null &
echo "started runner (pid $!); logging to $LOG"
