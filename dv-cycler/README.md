# DV matrix cycler

A small Go program (`package main`, `dv-cycler/main.go`) that runs every
static args-file in `network-params/` (one per CL/VC pairing behind the DV
client) 24/7 on a host machine, using the local Kurtosis `ethereum-package`
harness.

## Static param files (`network-params/`)

Unlike an earlier design that generated each run's args-file from code, the
cycler now just runs whatever `*.yaml` files exist in `network-params/`
(default: `<CYCLER_REPO_PATH>/dv-cycler/network-params`, overridable via
`CYCLER_PARAMS_DIR`). Each file is a complete, self-contained
ethereum-package args-file (participants, network params, MEV, monitoring,
etc.) with client/DV image pins inlined and a literal
`$PROMETHEUS_REMOTE_WRITE_TOKEN` placeholder that the cycler substitutes at
runtime with `CYCLER_MONITORING_TOKEN`.

The directory is re-scanned (sorted lexically) on every loop iteration, so:

- **Adding a file picks it up automatically.** Drop a 37th file into
  `network-params/` (e.g. `newclient-teku.yaml`) and it runs in its turn on
  the very next pass through the directory — no cycler restart needed.
- **Bumping a pin means editing that file.** There's no shared `images.json`
  for the cycler anymore — each `network-params/*.yaml` carries its own
  pins. Edit the file, commit, push to `CYCLER_REPO_PATH`; the next time
  that file comes up in rotation (after the cycler's per-run `git pull`) it
  launches with the new pin.
- **Removing a file removes it from rotation** the same way, on the next
  full pass.

`cluster_name` is *not* baked into the file or derived from its name — the
cycler discovers it at runtime (see below), since it's an emergent property
of the running enclave, not something the static file declares.

## What it does (the run cycle)

For each param file in the directory, in sorted order, forever:

1. `git pull` the repo (`CYCLER_REPO_PATH`) so the run always picks up the
   latest committed param files (pins, structure, or newly added/removed
   files) before launching.
2. Tear down any stale enclave left over from a previous crash, substitute
   the Prometheus remote-write token into the file's contents, and launch a
   fresh Kurtosis enclave from the resulting temp args-file (native
   Kurtosis path, no DV-specific harness changes).
3. Wait for the cluster to become healthy (bounded by
   `CYCLER_STARTUP_DEADLINE_MINUTES`); if it never comes up, the run is
   recorded as `failed`.
4. Discover the enclave's charon `cluster_name` at runtime by querying
   Prometheus (there is exactly one charon cluster per enclave — the
   bootstrap lighthouse VCs are not charon and don't count).
5. Sample host CPU/mem for the duration of the run (`CYCLER_RUN_MINUTES`,
   default 90) at `CYCLER_SAMPLE_INTERVAL_S` intervals.
6. At the end of the window, query local Prometheus (scoped to the
   discovered `cluster_name`) for duty success ratios (worst node), DV
   CPU/mem peaks, and `app_health_checks` firing status. The scoring window
   excludes the first `CYCLER_WARMUP_MINUTES` of the run so startup noise
   doesn't count against the run.
7. Post one Slack message (via Incoming Webhook) summarizing the run: the
   param file's name, the discovered cluster, status (`ok` / `degraded` /
   `failed`), worst-node duty ratios, DV CPU/mem peaks, host stats, and any
   firing health checks.
8. Tear down the enclave (best-effort/idempotent) and advance to the next
   file, backing off (`CYCLER_INTER_RUN_BACKOFF_S`, capped at
   `CYCLER_MAX_BACKOFF_S`) after consecutive failures.

A failure at any stage (launch, health wait, cluster discovery, sampling,
metrics query, report assembly) produces a `failed` Slack report instead of
crashing the loop — the cycler always continues to the next file. An empty
or unreadable `network-params/` directory is treated the same way: a
warning is logged, the loop backs off, and it retries (re-scanning the
directory) rather than exiting. Slack-post failures are swallowed too; they
must never block teardown.

## Running it

No build step is required — the cycler is run directly with `go run`, from
the module directory. Put config in a `.env` file:

```bash
cd dv-cycler
cp .env.example .env      # then edit .env (webhook, paths, token)
go run .
```

Or set the `CYCLER_*` variables in the environment (these override `.env`):

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

### Start/stop scripts

For a host without a supervisor, three helper scripts wrap the manual launch:

```bash
./start.sh    # launch detached (setsid+nohup; survives logout); idempotent
./status.sh   # show process, enclaves, and recent log (webhook masked)
./stop.sh     # stop the loop AND tear down the current run's enclave
```

`start.sh` reads `.env` from this directory, logs to `$CYCLER_LOG` (default
`~/dv-cycler.log`), and adds `$HOME/sdk/go/bin` or `/usr/local/go/bin` to `PATH`
if `go` isn't already resolvable. `stop.sh` stops the loop process and tears
down the in-flight enclave; the state file is preserved, so a later `start.sh`
resumes at the same rotation position. These are a convenience for
manual operation; for unattended 24/7 use prefer the systemd unit below.

## Configuration

Configuration is via `CYCLER_*` environment variables. At startup the cycler
also loads a `.env` file (`KEY=value`) from its working directory — copy
`.env.example` to `.env` and fill it in (point elsewhere with
`CYCLER_ENV_FILE`). Real environment variables and `--flags` take precedence
over `.env`, and `.env` is gitignored so secrets stay out of the repo.
`loadConfig()` returns an error naming every missing required key if any of
the three required variables is unset or empty.

| Env var | Required | Default | Description |
|---|---|---|---|
| `CYCLER_SLACK_WEBHOOK_URL` | yes | — | Slack Incoming Webhook URL for run reports. |
| `CYCLER_REPO_PATH` | yes | — | Absolute path to this repo checkout on the host (the cycler `git pull`s this path before each run). |
| `CYCLER_STATE_PATH` | yes | — | Absolute path to the state file (see below); the containing directory must exist and be writable by the service user. |
| `CYCLER_PARAMS_DIR` | no | `<CYCLER_REPO_PATH>/dv-cycler/network-params` | Directory scanned for `*.yaml` param files every loop iteration. |
| `CYCLER_MONITORING_TOKEN` | no | `""` | Prometheus remote-write auth token (substituted for `$PROMETHEUS_REMOTE_WRITE_TOKEN` in each param file); empty disables remote-write auth. |
| `CYCLER_PACKAGE_REF` | no | `github.com/ObolNetwork/ethereum-package@charon` | Kurtosis package reference to run. |
| `CYCLER_RUN_MINUTES` | no | `90` | Length of each run's window. |
| `CYCLER_WARMUP_MINUTES` | no | `15` | Leading portion of the run window excluded from duty/health scoring. |
| `CYCLER_STARTUP_DEADLINE_MINUTES` | no | `25` | How long to wait for the enclave to become healthy before recording the run `failed`. |
| `CYCLER_SAMPLE_INTERVAL_S` | no | `15` | Host CPU/mem sampling interval during the run. |
| `CYCLER_INTER_RUN_BACKOFF_S` | no | `30` | Base backoff between runs after a failure. |
| `CYCLER_MAX_BACKOFF_S` | no | `900` | Cap on the (doubling) backoff after consecutive failures. |

## Running as a systemd service (optional)

For unattended 24/7 operation, run it under a supervisor. There's no committed
unit file — create one like the following, adjusting `User`, the paths, and the
`go` binary location for your host:

```ini
[Unit]
Description=DV 36-combo test cycler
After=docker.service network-online.target
Wants=docker.service network-online.target

[Service]
Type=simple
User=dv
WorkingDirectory=/opt/kurtosis-charon/dv-cycler
Environment=GOCACHE=/var/cache/dv-cycler/go-build
Environment=HOME=/opt/kurtosis-charon
ExecStart=/usr/local/go/bin/go run .
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

Save it as `/etc/systemd/system/dv-cycler.service`, then
`sudo systemctl daemon-reload && sudo systemctl enable --now dv-cycler`.

Notes:

- **Config comes from `.env`** in `WorkingDirectory` — create it there from
  `.env.example` before enabling; the unit carries no secrets.
- **`GOCACHE`/`HOME` must exist and be writable by `User=`.** `go run .`
  compiles into the build cache on every start (no prebuilt binary) and Go
  touches `$HOME/.cache`; if these aren't writable the unit crash-loops.
- **`Restart=always` + `RestartSec=30`** brings it back on crash/reboot; the
  state file (below) makes resume safe.

## Logs

```bash
journalctl -u cycler -f
```

Since the unit sets `StandardOutput=journal` / `StandardError=journal`, all
cycler output (including per-run status) goes to the systemd journal under
the `cycler` unit.

## State file and resume behavior

The cycler persists its position in the `network-params/` rotation to
`CYCLER_STATE_PATH` after every run (`cycle`, `next_index`, and — while a run
is in flight — `current_enclave`). On startup, `mainLoop`:

- Loads `CYCLER_STATE_PATH` if it exists (otherwise starts fresh at cycle 0,
  index 0).
- If `current_enclave` is set (meaning the previous process died mid-run), it
  tears down that stale enclave and clears the field before resuming.
- Resumes from `next_index`/`cycle` rather than starting over. Since the
  directory is re-scanned every iteration, `next_index` is clamped against
  the current file count each time (if a file was removed and `next_index`
  now runs past the end of the list, the cycler wraps to index 0 and bumps
  `cycle` rather than erroring).

This means a reboot or systemd restart loses at most the in-flight run — the
service picks back up where it left off instead of restarting the whole
rotation.

## Running tests

```bash
cd dv-cycler
go test ./...
```
