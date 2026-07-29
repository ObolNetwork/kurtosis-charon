# Charon matrix cycler

A small Python service that runs the Charon 36-combo test matrix (6 CL clients x
6 VC clients: `lighthouse`, `lodestar`, `nimbus`, `teku`, `prysm`, `grandine` for
CL, and the same six plus `vouch` swapped in for VC — see
`charon_matrix/network_params.py` for the authoritative lists) 24/7 on a
host machine, using the local Kurtosis `ethereum-package` harness.

## What it does (the run cycle)

For each combo in the cycle, in sequence, forever:

1. `git pull` the repo (`repo_path` in the config) so the run always picks up
   the latest client/Charon version pins and any code changes.
2. Tear down any stale enclave left over from a previous crash, then launch a
   fresh 4-node Kurtosis enclave for the combo (native Kurtosis path, no
   Charon-specific harness changes).
3. Wait for the cluster to become healthy (bounded by
   `startup_deadline_minutes`); if it never comes up, the run is recorded as
   `failed`.
4. Sample host CPU/mem for the duration of the run (`run_minutes`, default 90)
   at `sample_interval_s` intervals.
5. At the end of the window, query local Prometheus for duty success ratios
   (worst node), Charon CPU/mem peaks, and `app_health_checks` firing status.
   The scoring window excludes the first `warmup_minutes` of the run so
   startup noise doesn't count against the combo.
6. Post one Slack message (via Incoming Webhook) summarizing the run: combo,
   images, status (`ok` / `degraded` / `failed`), worst-node duty ratios,
   Charon CPU/mem peaks, host stats, and any firing health checks.
7. Tear down the enclave (best-effort/idempotent) and advance to the next
   combo.

A failure at any stage (launch, health wait, sampling, metrics query, report
assembly) produces a `failed` Slack report instead of crashing the loop — the
cycler always continues to the next combo. Slack-post failures are swallowed
too; they must never block teardown.

## Version pins

Client and Charon image pins live in `charon_matrix/network_params.py` at the
repo root (`CHARON_IMAGE`, `CL_IMAGES`, `VC_IMAGES`, `CLS`, `VCS`). To bump a
version:

1. Edit the pin in `charon_matrix/network_params.py`.
2. Commit and push to `repo_path`.

The cycler `git pull`s `repo_path` before every single run, so the next combo
launched after your push will pick up the new pin automatically — no restart
of the cycler service required.

## Install

```bash
cd charon-cycler
python -m venv .venv
.venv/bin/pip install pyyaml   # runtime dependency
.venv/bin/pip install pytest   # only needed to run the test suite

cp config.example.yaml config.yaml
```

Edit `config.yaml` and set the following. `load_config()` raises `KeyError` at
startup if any of the three **required** keys is missing:

Required:

- `slack_webhook_url` — Slack Incoming Webhook URL for run reports.
- `repo_path` — absolute path to this repo checkout on the host (the cycler
  `git pull`s this path before each run).
- `state_path` — absolute path to the state file (see below); the containing
  directory must exist and be writable by the service user.

Optional:

- `monitoring_token` — the Prometheus remote-write auth token
  (`PROMETHEUS_REMOTE_WRITE_TOKEN`); defaults to `""`, which disables
  remote-write auth. Not required to start the cycler.

The remaining keys (`package_ref`, `run_minutes`, `warmup_minutes`,
`startup_deadline_minutes`, `sample_interval_s`) have sane defaults in
`config.example.yaml` and normally don't need to change.

> Note: if the repo's own top-level `.venv` is broken/unusable, create a fresh
> venv as above rather than trying to repair it — the cycler only needs
> `pyyaml` at runtime and `pytest` for tests.

## systemd install

```bash
sudo cp cycler.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now cycler
```

`cycler.service` runs as the `charon` user out of
`/opt/kurtosis-charon/charon-cycler`, using that directory's
`.venv/bin/python -m charon_cycler.cycler config.yaml`. Adjust the paths in
`cycler.service` if your checkout lives elsewhere. `Restart=always` with
`RestartSec=30` means the service comes back on crash or reboot; state/resume
behavior (below) makes that safe.

**Important — PYTHONPATH:** `cycler.py` does
`from charon_matrix.network_params import ...`, and `charon_matrix` lives one
level up from `charon-cycler`, at the repo root
(`/opt/kurtosis-charon/charon_matrix`). Running
`python -m charon_cycler.cycler` only puts the current working directory on
`sys.path`, which makes `charon_cycler` importable but *not* `charon_matrix`.
Without both directories on `PYTHONPATH`, the service crash-loops with
`ModuleNotFoundError: No module named 'charon_matrix'`. `cycler.service`
therefore sets:

```ini
Environment="PYTHONPATH=/opt/kurtosis-charon:/opt/kurtosis-charon/charon-cycler"
```

If your checkout lives somewhere other than
`/opt/kurtosis-charon`, update both this `Environment=` line and the
`WorkingDirectory`/`ExecStart` paths to match — `PYTHONPATH` must include the
repo root (for `charon_matrix`) and the `charon-cycler` directory (for
`charon_cycler`).

## Logs

```bash
journalctl -u cycler -f
```

Since the unit sets `StandardOutput=journal` / `StandardError=journal`, all
cycler output (including per-run status and any tracebacks caught at the top
level) goes to the systemd journal under the `cycler` unit.

## State file and resume behavior

The cycler persists its position in the 36-combo cycle to `state_path` after
every run (`cycle`, `next_index`, and — while a run is in flight —
`current_enclave`). On startup, `main()`:

- Loads `state_path` if it exists (otherwise starts fresh at cycle 0, index 0).
- If `current_enclave` is set (meaning the previous process died mid-run), it
  tears down that stale enclave and clears the field before resuming.
- Resumes the cycle from `next_index`/`cycle` rather than starting over.

This means a reboot or systemd restart loses at most the in-flight run — the
service picks back up where it left off instead of restarting the whole
36-combo matrix.

## Priority-override extension point (not built yet)

`charon_cycler/selection.py` has a `read_override()` stub that always
returns `None`, so `select_next_combo()` currently always falls through to the
normal cycle order. It's the designed extension point for a future
"run this combo next / pin this combo" feature: a later implementation would
have `read_override()` read a `charon-cycler/override.json` (or similar)
file and return `{"cl": ..., "vc": ..., "sticky": ...}` to jump the cycle to
that combo. `select_next_combo()` already has the override branch wired up —
only the reader needs to be implemented.

## Running tests

```bash
cd charon-cycler
.venv/bin/pytest
```
