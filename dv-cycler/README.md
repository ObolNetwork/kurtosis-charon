# DV matrix cycler

A small Go program (`package main`, `dv-cycler/main.go`) that runs the
DV 36-combo test matrix (6 CL clients x 6 VC clients: `lighthouse`,
`lodestar`, `nimbus`, `teku`, `prysm`, `grandine` for CL, and the same six
plus `vouch` swapped in for VC) 24/7 on a host machine, using the local
Kurtosis `ethereum-package` harness.

## What it does (the run cycle)

For each combo in the cycle, in sequence, forever:

1. `git pull` the repo (`CYCLER_REPO_PATH`) so the run always picks up the
   latest client/DV version pins and any code changes.
2. Tear down any stale enclave left over from a previous crash, then launch a
   fresh 4-DV-node Kurtosis enclave for the combo (native Kurtosis path,
   no DV-specific harness changes).
3. Wait for the cluster to become healthy (bounded by
   `CYCLER_STARTUP_DEADLINE_MINUTES`); if it never comes up, the run is
   recorded as `failed`.
4. Sample host CPU/mem for the duration of the run (`CYCLER_RUN_MINUTES`,
   default 90) at `CYCLER_SAMPLE_INTERVAL_S` intervals.
5. At the end of the window, query local Prometheus for duty success ratios
   (worst node), DV CPU/mem peaks, and `app_health_checks` firing status.
   The scoring window excludes the first `CYCLER_WARMUP_MINUTES` of the run so
   startup noise doesn't count against the combo.
6. Post one Slack message (via Incoming Webhook) summarizing the run: combo,
   images, status (`ok` / `degraded` / `failed`), worst-node duty ratios,
   DV CPU/mem peaks, host stats, and any firing health checks.
7. Tear down the enclave (best-effort/idempotent) and advance to the next
   combo, backing off (`CYCLER_INTER_RUN_BACKOFF_S`, capped at
   `CYCLER_MAX_BACKOFF_S`) after consecutive failures.

A failure at any stage (launch, health wait, sampling, metrics query, report
assembly) produces a `failed` Slack report instead of crashing the loop — the
cycler always continues to the next combo. Slack-post failures are swallowed
too; they must never block teardown.

## Running it

No build step is required — the cycler is run directly with `go run`, from
the module directory:

```bash
cd dv-cycler
CYCLER_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... \
CYCLER_REPO_PATH=/opt/kurtosis-charon \
CYCLER_STATE_PATH=/var/lib/dv-cycler/state.json \
go run .
```

Requires a Go toolchain (matching `go.mod`'s `go 1.26` directive or newer) on
the host; `go run .` compiles and runs `main.go` on every invocation, so no
binary is committed or needs to be rebuilt after a `git pull`.

## Configuration

All configuration is via `CYCLER_*` environment variables (there is no config
file). `loadConfig()` returns an error naming every missing required key if
any of the three required variables is unset or empty.

| Env var | Required | Default | Description |
|---|---|---|---|
| `CYCLER_SLACK_WEBHOOK_URL` | yes | — | Slack Incoming Webhook URL for run reports. |
| `CYCLER_REPO_PATH` | yes | — | Absolute path to this repo checkout on the host (the cycler `git pull`s this path before each run, and reads `images.json` from it every run). |
| `CYCLER_STATE_PATH` | yes | — | Absolute path to the state file (see below); the containing directory must exist and be writable by the service user. |
| `CYCLER_MONITORING_TOKEN` | no | `""` | Prometheus remote-write auth token (`PROMETHEUS_REMOTE_WRITE_TOKEN`); empty disables remote-write auth. |
| `CYCLER_PACKAGE_REF` | no | `github.com/ObolNetwork/ethereum-package@charon` | Kurtosis package reference to run. |
| `CYCLER_RUN_MINUTES` | no | `90` | Length of each combo's run window. |
| `CYCLER_WARMUP_MINUTES` | no | `15` | Leading portion of the run window excluded from duty/health scoring. |
| `CYCLER_STARTUP_DEADLINE_MINUTES` | no | `25` | How long to wait for the enclave to become healthy before recording the run `failed`. |
| `CYCLER_SAMPLE_INTERVAL_S` | no | `15` | Host CPU/mem sampling interval during the run. |
| `CYCLER_INTER_RUN_BACKOFF_S` | no | `30` | Base backoff between runs after a failure. |
| `CYCLER_MAX_BACKOFF_S` | no | `900` | Cap on the (doubling) backoff after consecutive failures. |

## Version pins

Client and DV image pins live in `images.json` at the repo root
(`dv`, `el`, `bootstrap_cl`, `cl`, `vc`). To bump a version:

1. Edit the pin in `images.json`.
2. Commit and push to `CYCLER_REPO_PATH`.

The cycler `git pull`s `CYCLER_REPO_PATH` before every single run and reads
`images.json` fresh each time (`loadImages`), so the next combo launched
after your push picks up the new pin automatically — no restart of the
cycler service required.

## systemd install

```bash
sudo cp cycler.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cycler
```

`cycler.service` runs as the `dv` user out of
`/opt/kurtosis-charon/dv-cycler`, via `go run .`. Adjust `User`,
`WorkingDirectory`, the `Environment=` paths, and the `go` path in
`ExecStart` if your checkout, service account, or Go install location differ.
`Restart=always` with `RestartSec=30` means the service comes back on crash
or reboot; state/resume behavior (below) makes that safe.

**Important — `GOCACHE`/`HOME`:** `go run .` compiles the program into the
build cache on every start (there's no prebuilt binary), so the service user
needs a writable `GOCACHE` and, in practice, a writable `HOME` (Go also
touches `$HOME/.cache` and module-related state by default). `cycler.service`
therefore sets:

```ini
Environment=GOCACHE=/var/cache/dv-cycler/go-build
Environment=HOME=/opt/kurtosis-charon
```

Both directories must exist and be writable by the `User=` the service runs
as, or `go run` fails immediately and the unit crash-loops. If you change
`User=` or relocate the checkout, update these paths (and their on-disk
permissions) to match.

## Logs

```bash
journalctl -u cycler -f
```

Since the unit sets `StandardOutput=journal` / `StandardError=journal`, all
cycler output (including per-run status) goes to the systemd journal under
the `cycler` unit.

## State file and resume behavior

The cycler persists its position in the 36-combo cycle to `CYCLER_STATE_PATH`
after every run (`cycle`, `next_index`, and — while a run is in flight —
`current_enclave`). On startup, `mainLoop`:

- Loads `CYCLER_STATE_PATH` if it exists (otherwise starts fresh at cycle 0,
  index 0).
- If `current_enclave` is set (meaning the previous process died mid-run), it
  tears down that stale enclave and clears the field before resuming.
- Resumes the cycle from `next_index`/`cycle` rather than starting over.

This means a reboot or systemd restart loses at most the in-flight run — the
service picks back up where it left off instead of restarting the whole
36-combo matrix.

## Running tests

```bash
cd dv-cycler
go test ./...
```
