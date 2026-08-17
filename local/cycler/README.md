# DV matrix cycler

A small Go program (`package main`, `local/cycler/main.go`) that runs every
static args-file in `deployments/` (one per CL/VC pairing behind the DV
client) 24/7 on a host machine, using the local Kurtosis `ethereum-package`
harness.

## Static param files (`deployments/`)

Unlike an earlier design that generated each run's args-file from code, the
cycler now just runs whatever `*.yaml` files exist in `deployments/`
(default: `<CYCLER_REPO_PATH>/deployments`, overridable via
`CYCLER_PARAMS_DIR`). Each file is a complete, self-contained
ethereum-package args-file (participants, network params, MEV, monitoring,
etc.) with client/DV image pins inlined and a literal
`$PROMETHEUS_REMOTE_WRITE_TOKEN` placeholder that the cycler substitutes at
runtime with `CYCLER_MONITORING_TOKEN`.

The directory is re-scanned (sorted lexically) on every loop iteration, so:

- **Adding a file picks it up automatically.** Drop a 37th file into
  `deployments/` (e.g. `newclient-teku.yaml`) and it runs in its turn on
  the very next pass through the directory — no cycler restart needed.
- **Bumping a pin means editing that file.** There's no shared `images.json`
  for the cycler anymore — each `deployments/*.yaml` carries its own
  pins. Edit the file, commit, push to `CYCLER_REPO_PATH`; the next time
  that file comes up in rotation (after the cycler's per-run `git pull`) it
  launches with the new pin.
- **Moving tags are always re-pulled.** The cycler runs `kurtosis run` with
  `--image-download always`, so a moving tag like `obolnetwork/charon:next`
  picks up a freshly-built image on the next run rather than reusing a stale
  locally-cached one (Kurtosis's default `missing` policy would never re-pull
  it). A rebuilt `:next` therefore lands on the next combo launch — no param
  file edit needed.
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
or unreadable `deployments/` directory is treated the same way: a
warning is logged, the loop backs off, and it retries (re-scanning the
directory) rather than exiting. Slack-post failures are swallowed too; they
must never block teardown.

## Failure log capture

When a run ends **non-`ok`** (`failed` or `degraded`), the cycler captures the
logs of the relevant services *before tearing the enclave down* — the DV
participant's own beacon node, all Charon nodes (`*-charon-charon-<N>`), and all
DV validator clients (`*-charon-vc-*`) — and writes them to a gzipped tarball
under `CYCLER_LOG_DIR` named `cycle<N>-<combo>-<UTC-timestamp>.tar.gz`. The Slack
report for that run includes the archive path and a short excerpt (recent
error/warn/fatal lines from a Charon node).

The beacon node captured is the one the Charon cluster actually uses — the CL
sharing the Charon nodes' participant index (`vc-3-…` → `cl-3-…`), *not* the
fixed lighthouse bootstrap node (`cl-1`), which is a different client than most
combos. Containers are scoped to the current enclave via kurtosis's
`enclave-name` label, so a leftover container from a prior run is never
captured.

Capture is best-effort — a problem gathering logs never breaks the run or the
loop. It relies on `docker` being available to the service user and assumes a
single enclave is running (true for the cycler, which tears down between runs),
so it scopes targets by service-name pattern.

If `CYCLER_SLACK_BOT_TOKEN` and `CYCLER_SLACK_CHANNEL_ID` are set, the archive
is also uploaded to that Slack channel via the Web API
(`files.getUploadURLExternal` → upload → `files.completeUploadExternal`). The
incoming webhook used for the report itself cannot attach files, so without a
bot token the logs are saved locally only (the report still links the path).

**Local cleanup:** on a *successful* upload the local archive is **deleted** —
Slack becomes the durable store, so `CYCLER_LOG_DIR` doesn't grow over time. If
upload isn't configured, or an upload fails, the local archive is **kept** (it's
then the only copy). So a healthy, upload-configured deployment leaves
`CYCLER_LOG_DIR` empty between failures; anything lingering there is a run whose
upload didn't go through.

## Results matrix

Alongside the per-run reports, the cycler maintains a **single persistent
matrix** (one row per combo) in `CYCLER_RESULTS_PATH`, so it survives restarts.
Each row holds the combo's latest result — status, duty success %, Charon peak
mem/cpu, host peak cpu/mem — plus the **versions it ran with**: the DV
beacon-node image (`cl`), the Charon-managed VC image (`vc`), and the **Charon
git commit** (`charon`, captured from `charon version` in the container).

**Invalidation & re-test.** On each `git pull` the cycler diffs the current
param-file pins (each combo's DV `cl_image` and `charon_vc_image`) against what
each row last ran with. Any combo whose CL or VC version changed is
**invalidated**, and the scheduler **prioritises** those combos (runs them
ahead of the normal rotation) so a version bump is re-tested quickly. A combo
counts as filled once it has run on the current pins, regardless of
ok/degraded/failed (a persistently-failing combo shows its failure but doesn't
block completion). Example: bumping Lodestar (CL **and** VC) invalidates the 11
combos where Lodestar is the beacon node or the VC.

**Posting.** Once every combo is valid again (the matrix is whole), the cycler
posts the full matrix to Slack as a **fresh message** (via the webhook) —
columns `combo | cl | vc | charon | status | duty% | chn-mem | chn-cpu |
host-cpu | host-mem`, one message (the wide table is split across code blocks
to fit Slack's limits). So you get a new, notified matrix each time a version
change finishes testing; normal rotation between changes refreshes rows quietly
without re-posting. A new Charon `:next` build is *not* an invalidation trigger
— it only updates the `charon` column opportunistically as combos re-run. Set
`CYCLER_SUMMARY_MENTION` to a Slack mention (e.g. `<!subteam^S123>`) to prepend
a ping to the matrix message.

## Running it

No build step is required — the cycler is run directly with `go run`, from
the module directory. Put config in a `.env` file:

```bash
cd local/cycler
cp .env.example .env      # then edit .env (webhook, paths, token)
go run .
```

Or set the `CYCLER_*` variables in the environment (these override `.env`):

```bash
cd local/cycler
CYCLER_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... \
CYCLER_REPO_PATH=/opt/kurtosis-charon \
CYCLER_STATE_PATH=/var/lib/cycler/state.json \
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
`~/cycler.log`), and adds `$HOME/sdk/go/bin` or `/usr/local/go/bin` to `PATH`
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
| `CYCLER_PARAMS_DIR` | no | `<CYCLER_REPO_PATH>/deployments` | Directory scanned for `*.yaml` param files every loop iteration. |
| `CYCLER_MONITORING_TOKEN` | no | `""` | Prometheus remote-write auth token (substituted for `$PROMETHEUS_REMOTE_WRITE_TOKEN` in each param file); empty disables remote-write auth. |
| `CYCLER_PACKAGE_REF` | no | `github.com/ObolNetwork/ethereum-package@charon` | Kurtosis package reference to run. |
| `CYCLER_LOG_DIR` | no | `<home>/cycler-logs` | Directory where failing-run log archives (`.tar.gz`) are written. |
| `CYCLER_SLACK_BOT_TOKEN` | no | `""` | Slack bot token (`files:write` scope) for uploading failing-run log archives. Empty = local save only. |
| `CYCLER_SLACK_CHANNEL_ID` | no | `""` | Slack channel id to upload failing-run log archives into (needs `CYCLER_SLACK_BOT_TOKEN`). |
| `CYCLER_RESULTS_PATH` | no | `<state-dir>/cycler-results.json` | Persistent results-matrix file (one row per combo + versions). |
| `CYCLER_SUMMARY_MENTION` | no | `""` | Slack mention prepended to the matrix message (e.g. `<!subteam^S123>`); empty = no ping. |
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
WorkingDirectory=/opt/kurtosis-charon/local/cycler
Environment=GOCACHE=/var/cache/cycler/go-build
Environment=HOME=/opt/kurtosis-charon
ExecStart=/usr/local/go/bin/go run .
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

Save it as `/etc/systemd/system/cycler.service`, then
`sudo systemctl daemon-reload && sudo systemctl enable --now cycler`. The
`WorkingDirectory` is the Go module root (`local/cycler/`), not the repo root.

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

The cycler persists its position in the `deployments/` rotation to
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
cd local/cycler
go test ./...
```
