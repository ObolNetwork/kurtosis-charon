# Charon Cycler — Go Port (script-like) — Design

**Date:** 2026-07-29
**Repo:** `ObolNetwork/kurtosis-charon`, branch `kalo/native-36-matrix`
**Replaces:** the Python `charon-cycler/charon_cycler/` package (and its tests).

## Goal

Reimplement the 24/7 sequential 36-combo Charon cycler in **Go**, run directly
with `go run .` — no Python, no venv, no committed build artifact. Behaviour is
identical to the Python implementation (see
`2026-07-29-charon-36-cycler-design.md`); this is a language port, not a redesign.

**Execution model:** the service runs via `go run .` (the host already has the
Go toolchain). No binary is built or committed. Modern `go run` forwards
`SIGTERM`/`SIGINT` to the child, so systemd `stop`/`Restart=always` work
normally. Caveats, all handled in the unit file: the service user needs a
writable `GOCACHE`/`HOME` (one `Environment=` line), and each (re)start
recompiles for ~1–3s (negligible for a process that runs for days).

## Guiding constraint: keep it script-like

The user explicitly wants a small, script-like program — **not** a layered "project".

- **Everything in `package main`.** No `internal/` package tree.
- **Prefer a single `main.go`** holding all code. Split into at most 2–3
  `package main` files only if one file becomes genuinely unwieldy (>~900 lines);
  never introduce packages to organize it.
- **No interfaces, no DI framework, no "OO" layering.** Plain functions and
  structs (structs only as plain data records).
- **I/O seams are package-level function variables**, not interfaces:
  `var runCommand = func(name string, args ...string) (string, error) {…}`,
  `var httpGet = func(url string) ([]byte,int,error){…}`,
  `var httpPost = func(url string, body []byte) (int,error){…}`,
  `var nowFn = time.Now`, `var sleepFn = time.Sleep`,
  `var readFileFn = os.ReadFile`. Tests reassign these to fakes and restore them.
- **Stdlib only** — zero external modules. (JSON via `encoding/json`, args-file
  via `text/template` or plain string building, HTTP via `net/http`, subprocess
  via `os/exec`, `/proc` via `os.ReadFile`.)

## Layout

```
charon-cycler/
  go.mod                # module github.com/ObolNetwork/kurtosis-charon/charon-cycler, go 1.26, no requires
  main.go               # ALL program code, package main
  main_test.go          # table-driven tests for the pure functions + func-var fakes
  cycler.service        # systemd unit (ExecStart = `go run .`; GOCACHE/HOME set; no PYTHONPATH)
  README.md             # run (go run)/deploy/version-bump/override docs
images.json             # repo root: shared image pins (NEW; also read by the Python AWS runner)
```

The Python `charon-cycler/charon_cycler/`, `charon-cycler/tests/`,
`charon-cycler/pyproject.toml`, and `charon-cycler/config.example.yaml` are deleted.

## `images.json` (single source of truth for pins)

New repo-root file, e.g.:

```json
{
  "charon": "obolnetwork/charon:next",
  "el": "ethereum/client-go:v1.17.4",
  "bootstrap_cl": "sigp/lighthouse:v8.2.1",
  "cl": {
    "lighthouse": "sigp/lighthouse:v8.2.1",
    "lodestar": "chainsafe/lodestar:v1.45.0",
    "nimbus": "statusim/nimbus-eth2:multiarch-v26.7.0",
    "teku": "consensys/teku:26.7.1",
    "prysm": "gcr.io/prysmaticlabs/prysm/beacon-chain:v7.1.8",
    "grandine": "sifrai/grandine:2.0.5"
  },
  "vc": {
    "lighthouse": "sigp/lighthouse:v8.2.1",
    "lodestar": "chainsafe/lodestar:v1.45.0",
    "nimbus": "statusim/nimbus-validator-client:multiarch-v26.7.0",
    "teku": "consensys/teku:26.7.1",
    "prysm": "gcr.io/prysmaticlabs/prysm/validator:v7.1.8",
    "vouch": "attestant/vouch:1.13.1"
  }
}
```

- The Go cycler reads `<repo_path>/images.json` **at runtime**, after each
  `git pull`, so version bumps land without rebuilding the binary.
- The combo matrix (`CLS`/`VCS`) is derived from the JSON key order? No — key
  order isn't stable in JSON. The matrix order is fixed in Go
  (`cls := []string{"lighthouse","lodestar","nimbus","teku","prysm","grandine"}`,
  `vcs := []string{"lighthouse","lodestar","nimbus","teku","prysm","vouch"}`),
  CL-major; `images.json` only supplies the pin strings looked up by name.
- The static network params (mnemonic, network id, seconds_per_slot, etc.) stay
  as Go constants in `main.go` (they don't get bumped); only images live in the file.
- **Python side:** `charon_matrix/network_params.py` is refactored to
  `json.load` `images.json` (path resolved relative to the repo root) instead of
  hardcoding `CHARON_IMAGE`/`CL_IMAGES`/`VC_IMAGES`/`EL_IMAGE`/`BOOTSTRAP_CL_IMAGE`.
  `CLS`/`VCS` stay as Python lists (matrix order). The AWS runner keeps working.

## Config (env vars + flags, zero deps)

Read from environment, overridable by flags of the same name (lower-kebab).
Required: `CYCLER_SLACK_WEBHOOK_URL`, `CYCLER_REPO_PATH`, `CYCLER_STATE_PATH`
(error out if unset). Optional with defaults: `CYCLER_MONITORING_TOKEN` (""),
`CYCLER_PACKAGE_REF` (`github.com/ObolNetwork/ethereum-package@charon`),
`CYCLER_RUN_MINUTES` (90), `CYCLER_WARMUP_MINUTES` (15),
`CYCLER_STARTUP_DEADLINE_MINUTES` (25), `CYCLER_SAMPLE_INTERVAL_S` (15),
`CYCLER_INTER_RUN_BACKOFF_S` (30), `CYCLER_MAX_BACKOFF_S` (900).

## Update (static params)

The design below (code-generated args-file from `images.json` + a fixed
36-combo matrix) was superseded shortly after this doc was written. The
cycler no longer builds args-files or reads `images.json` at all: 36 static,
committed `dv-cycler/network-params/<cl>-<vc>.yaml` files now carry the full
args-file (pins inlined, `$PROMETHEUS_REMOTE_WRITE_TOKEN` placeholder
intact), and the cycler just enumerates `*.yaml` in that directory
(`CYCLER_PARAMS_DIR`, overridable) each loop iteration, substitutes the
token, and runs whatever it finds — so a 37th file dropped in runs on the
next pass with no code change. `cluster_name` is discovered at runtime via
Prometheus (`group by (cluster_name) (app_version)`) instead of being
derived from the combo. `images.json` and the Python `charon_matrix` runner
are unaffected. See `dv-cycler/README.md` for the current model.

## Behaviour (unchanged from the Python — carried over verbatim in intent)

- Sequential loop over the 36 combos (CL-major); resume from a JSON state file
  (`cycle`, `next_index`, `current_enclave`) with atomic write; on startup tear
  down any interrupted `current_enclave`.
- Per run: `git pull --ff-only` → write args-file (4 Charon nodes, token
  substituted) → `kurtosis run` → wait until healthy (deadline) → sample host
  CPU/mem for `run_minutes` → query the in-enclave Prometheus over
  `[run_minutes − warmup_minutes]` → post a Slack Block Kit report → tear down.
- Duty ratios from the **worst** Charon node (fewest successful duties); Charon
  CPU/mem = **max across nodes**; host totals from `/proc`; all firing
  `app_health_checks` listed; `degraded` uses a **99.5%** tolerance; window label
  is `end − window_s … end`.
- Failures are **reported, never fatal**: pre-launch (git/args) failures and any
  mid-run exception post a `failed` report; the enclave is **always** torn down
  (Go `defer`); the loop never dies; escalating inter-run backoff
  (`min(cap, base·2^consecutiveFailures)`) throttles a systemic outage.
- `readOverride()` returns nil (inert extension point).
- `PrometheusClient.query` errors (non-`success` status) surface as Go errors,
  handled by the run's failure path.

## Testing (no interfaces)

`main_test.go`, table-driven, covering the pure functions directly: worst-node
selection, `computeBackoff`, PromQL builders, `/proc` CPU/mem parsing, report
block building (ok/degraded/failed), config parsing (required/optional/defaults),
args-file generation (4 nodes + token subst + nimbus json_requests), state
round-trip + advance wrap, selection cycle-vs-override, and health parsing. I/O
glue (kurtosis arg construction, slack payload) is tested by swapping the
package-level `runCommand`/`httpPost` func vars for fakes that capture inputs.
Gate: `go vet ./...` clean, `go test ./...` green, and `go build ./...` compiles
as a sanity check (the built binary is discarded — the service runs via `go run .`).

## Cleanup / deliverables

- Delete the Python cycler package + tests + pyproject + config.example.yaml.
- Add `images.json`; refactor `charon_matrix/network_params.py` to read it.
- Update `charon-cycler/cycler.service` (ExecStart = `go run .`, `WorkingDirectory`
  = the module dir, `Environment=` for `GOCACHE`/`HOME` and the full `go` path,
  drop PYTHONPATH) and `charon-cycler/README.md` (run with `go run .`, env-var
  config, deploy, version bump via `images.json`, override hook).

## Out of scope

- Changing the Python AWS runner beyond the `images.json` refactor.
- Any behavioural change vs. the Python cycler.
- Porting the Python `charon_matrix` args-file generator to a shared Go/Python
  library (the two generators stay separate; only `images.json` is shared).
