# Charon Cycler — Go Port (script-like) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reimplement the Charon 36-combo cycler in Go as a single script-like `package main`, run via `go run .`, with behaviour identical to the committed Python cycler.

**Architecture:** One `main.go` in `package main` (stdlib only). Pure functions do the logic; I/O is behind package-level function variables (`runCommand`, `httpGet`, `httpPost`, `nowFn`, `sleepFn`, `readFileFn`) that tests reassign. Client image pins live in a repo-root `images.json` read at runtime (also read by the Python AWS runner). No interfaces, no sub-packages, no built binary.

**Tech Stack:** Go 1.26, standard library only (`encoding/json`, `net/http`, `os/exec`, `text/template` or string building, `time`, `flag`, `os`). Python 3 (only for the `charon_matrix` refactor). `go vet` + `go test` as the gate.

## Global Constraints

- **Script-like:** everything in `package main`; **prefer a single `main.go`**; split into at most 2–3 `package main` files only if one file exceeds ~900 lines; **never** add sub-packages, interfaces, or DI.
- **Stdlib only** — `go.mod` has no `require` block.
- **I/O seams are package-level `var fn = func(...)...`**, reassigned in tests; never interfaces.
- **Run via `go run .`** — no committed binary. `go build ./...` is only a compile check.
- **Behaviour parity with the Python cycler** (`charon-cycler/charon_cycler/*.py`, committed): worst-node duty ratios, max-across-nodes Charon CPU/mem, `/proc` host totals, all firing `app_health_checks`, warmup-excluded window, guaranteed enclave teardown (`defer`) + sampler stop, pre-launch + mid-run failures reported (never fatal), escalating backoff `min(cap, base·2^consecutiveFailures)`, **99.5%** degraded tolerance, window label `end−window_s … end`, atomic state + reboot resume, inert `readOverride`.
- **Matrix:** `cls := []string{"lighthouse","lodestar","nimbus","teku","prysm","grandine"}`, `vcs := []string{"lighthouse","lodestar","nimbus","teku","prysm","vouch"}`, CL-major, 36 combos. `clusterName = "kurtosis-"+cl+"-"+vc`; `enclaveName(cycle,cl,vc) = fmt.Sprintf("c%d-%s-%s", cycle, cl, vc)`.
- **charon_node_count = 4** in the generated args-file.
- **Config env vars** (flags of the same lower-kebab name override): required `CYCLER_SLACK_WEBHOOK_URL`, `CYCLER_REPO_PATH`, `CYCLER_STATE_PATH`; optional `CYCLER_MONITORING_TOKEN` (""), `CYCLER_PACKAGE_REF` (`github.com/ObolNetwork/ethereum-package@charon`), `CYCLER_RUN_MINUTES` (90), `CYCLER_WARMUP_MINUTES` (15), `CYCLER_STARTUP_DEADLINE_MINUTES` (25), `CYCLER_SAMPLE_INTERVAL_S` (15), `CYCLER_INTER_RUN_BACKOFF_S` (30), `CYCLER_MAX_BACKOFF_S` (900).
- **Commits:** no `Co-Authored-By` trailer; author is the repo user only.
- **Reference of record:** the committed Python at `charon-cycler/charon_cycler/` — port its behaviour exactly. It stays in the tree until Task 4.

---

## File Structure

```
kurtosis-charon/
  images.json                         # NEW (repo root): shared image pins
  charon_matrix/network_params.py     # MODIFIED: load images.json instead of hardcoding pins
  charon_matrix/tests/test_network_params.py  # MODIFIED: still green against images.json
  kurtosis-aws-runner/kurtosis_aws_runner_native.py  # unchanged (imports from charon_matrix)
  charon-cycler/
    go.mod                            # NEW: module, go 1.26, no requires
    main.go                           # NEW: all Go code, package main
    main_test.go                      # NEW: table-driven tests
    cycler.service                    # MODIFIED: ExecStart=go run .
    README.md                         # MODIFIED: go run docs
    charon_cycler/  tests/  pyproject.toml  config.example.yaml  # DELETED in Task 4
```

---

## Task 1: Shared images.json + Python refactor + Go module scaffold

**Files:**
- Create: `images.json` (repo root)
- Create: `charon-cycler/go.mod`
- Modify: `charon_matrix/network_params.py`
- Modify: `charon_matrix/tests/test_network_params.py`

**Interfaces:**
- Produces: `images.json` schema `{charon, el, bootstrap_cl, cl:{...}, vc:{...}}`; Python `build_network_params` unchanged in signature/output but sourcing pins from the file; a Go module `github.com/ObolNetwork/kurtosis-charon/charon-cycler`.

- [ ] **Step 1: Create `images.json`** (repo root), copying the exact pins currently in `charon_matrix/network_params.py`:

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

- [ ] **Step 2: Update the Python test first (TDD)** — `charon_matrix/tests/test_network_params.py`: keep the existing assertions, and add one proving the pins come from `images.json` (edit the file, run, see it fail if the module still hardcodes):

```python
def test_pins_come_from_images_json():
    import json, os
    from charon_matrix import network_params as np
    root = os.path.dirname(os.path.dirname(os.path.dirname(np.__file__)))
    data = json.load(open(os.path.join(root, "images.json")))
    assert np.CHARON_IMAGE == data["charon"]
    assert np.CL_IMAGES == data["cl"] and np.VC_IMAGES == data["vc"]
    assert data["cl"]["teku"] in np.build_network_params("teku", "prysm")
```

- [ ] **Step 3: Run it, expect failure** — `python -m pytest charon_matrix/tests -v` (from repo root, scratch venv). Expected: new test FAILS only if values drift; it will actually pass if identical — so first make the module load the file (Step 4), then this test guards against future drift. Treat Step 3 as "run the suite to confirm current green" and proceed.

- [ ] **Step 4: Refactor `network_params.py` to load `images.json`.** Replace the hardcoded `CHARON_IMAGE`, `EL_IMAGE`, `BOOTSTRAP_CL_IMAGE`, `CL_IMAGES`, `VC_IMAGES` with values loaded from the repo-root file; keep `CLS`, `VCS`, `VALIDATOR_KEYS_MNEMONIC`, and `build_network_params(cl, vc, charon_node_count=3)` exactly as-is otherwise:

```python
import json as _json
import os as _os

_IMAGES = _json.load(open(_os.path.join(_os.path.dirname(_os.path.dirname(__file__)), "images.json")))
CHARON_IMAGE = _IMAGES["charon"]
EL_IMAGE = _IMAGES["el"]
BOOTSTRAP_CL_IMAGE = _IMAGES["bootstrap_cl"]
CL_IMAGES = _IMAGES["cl"]
VC_IMAGES = _IMAGES["vc"]
```

(`__file__` is `charon_matrix/network_params.py`; one `dirname` → `charon_matrix/`, two → repo root where `images.json` lives.)

- [ ] **Step 5: Run Python tests + AWS-runner import check.**
Run: `python -m pytest charon_matrix/tests -v` → PASS.
Run: `python3 -c "import ast; ast.parse(open('kurtosis-aws-runner/kurtosis_aws_runner_native.py').read())"` and `python3 -c "import sys; sys.path.insert(0,'.'); from charon_matrix.network_params import build_network_params, CL_IMAGES; assert CL_IMAGES['teku']"` → no error.

- [ ] **Step 6: Create the Go module** — `charon-cycler/go.mod`:

```
module github.com/ObolNetwork/kurtosis-charon/charon-cycler

go 1.26
```

- [ ] **Step 7: Commit**

```bash
git add images.json charon_matrix/network_params.py charon_matrix/tests/test_network_params.py charon-cycler/go.mod
git commit -m "Add shared images.json; load pins from it in Python; scaffold Go module"
```

---

## Task 2: Go pure functions + tests (`main.go` / `main_test.go`)

Port the **pure logic** from the Python. Everything in `package main`. Add the I/O func-var declarations (bodies can be the real impls now; they're exercised in Task 3). Each function below maps to a Python source; match its behaviour exactly.

**Files:**
- Create/extend: `charon-cycler/main.go`
- Create/extend: `charon-cycler/main_test.go`

**Interfaces (produced — later tasks rely on these exact names/types):**

```go
// data records (plain structs)
type combo struct{ cl, vc string }
func (c combo) name() string        // "cl-vc"
func (c combo) clusterName() string // "kurtosis-cl-vc"
func enclaveName(cycle int, c combo) string // "c{cycle}-cl-vc"
var cycle []combo // 36, CL-major (package-level, built in init or a func)

type config struct {
    slackWebhookURL, repoPath, statePath, monitoringToken, packageRef string
    runMinutes, warmupMinutes, startupDeadlineMinutes, sampleIntervalS int
    interRunBackoffS, maxBackoffS int
}
func loadConfig() (config, error) // env + flags; error if required unset

type state struct{ Cycle, NextIndex int; CurrentEnclave string } // JSON tags: cycle,next_index,current_enclave
func loadState(path string) (state, error)   // missing file -> zero state, nil
func (s *state) save(path string) error       // atomic: tmp + os.Rename
func (s *state) advance()                      // next_index++, wrap at len(cycle) -> cycle++

func readOverride() *combo // inert: returns nil
func selectNextCombo(s state) (combo, string) // ("cycle"|"override"); override must not advance

func computeBackoff(consecutiveFailures, base, cap int) int // min(cap, base*2^n)

type images struct {
    Charon, EL, BootstrapCL string `json:"charon" ...`
    CL, VC map[string]string
}
func loadImages(repoPath string) (images, error) // reads <repoPath>/images.json
func buildArgsFile(im images, c combo, token string, charonNodeCount int) string // ethereum-package YAML

// promql builders (return string)
func promDutyExpected(clusterName string, windowS int) string
func promDutySuccess(clusterName string, windowS int) string
func promCharonMemPeak(clusterName string, windowS int) string
func promCharonCPUPeak(clusterName string, windowS int) string
func promHealthFired(clusterName string, windowS int) string
func promHealthFiringNow(clusterName string) string

// metrics aggregation
type sample struct{ labels map[string]string; value float64 }
type dutyResult struct{ duty string; expected, success float64 }
func (d dutyResult) pct() float64 // 0 if expected==0 else 100*success/expected
type worstNode struct{ peer string; duties []dutyResult }
func selectWorstNode(expected, success []sample) (worstNode, bool) // false if no peers
func maxValue(samples []sample) (float64, bool)
type healthCheck struct{ name, severity string; firingNow bool }
func parseHealth(fired, firingNow []sample) []healthCheck

// /proc parsing
func parseCPULine(text string) (busy, total float64) // total=sum, idle=idle+iowait, busy=total-idle
func cpuPercent(prev, cur [2]float64) float64         // 100*Δbusy/Δtotal, 0 if Δtotal<=0
func parseMeminfo(text string) (used, total float64)  // kB->bytes, used=MemTotal-MemAvailable

// report
type hostStats struct{ cpuAvg, cpuPeak, memAvg, memPeak, memTotal float64 }
type reportData struct { /* combo, cycle, status, clImage, vcImage, charonImage, window, *worstNode, charonMemBytes/CPU (*float64 or ok-bool), *hostStats, []healthCheck, errMsg */ }
func buildText(d reportData) string
func buildBlocks(d reportData) []map[string]any
```

Port these from, respectively: `combos.py`, `config.py`, `state.py`, `selection.py`, `combo`/backoff in `cycler.py` (`compute_backoff`), `params.py`+`network_params.py` (`buildArgsFile`), `promql.py`, `metrics.py`, `host_sampler.py`, `report.py`. Notes:
- `selectWorstNode`: worst = peer with the fewest total `success`; deterministic tie-break by `(totalSuccess, peer)`; a duty present in `expected` but absent from `success` must appear with `success=0` (see `metrics.py`).
- `buildArgsFile`: reproduce the exact YAML from `network_params.build_network_params` with `charon_node_count=4` and the `$PROMETHEUS_REMOTE_WRITE_TOKEN` replaced by `token`; nimbus VC adds the `vc_extra_env_vars: CHARON_FEATURE_SET_ENABLE: json_requests` block. Build via a raw string template; keep `storage_tsdb_retention_time: 3h`.
- `buildBlocks`: return `[]map[string]any` Block-Kit dicts; guard all optional fields (worstNode/host/mem/cpu nil → "n/a" / "_no duty data_"); `failed` surfaces `errMsg`.
- Represent optional numbers (charon mem/cpu, host) as pointers or `(float64, bool)` — pick one and use consistently.

- [ ] **Step 1: Write failing tests** in `main_test.go` for the pure functions. Table-driven; cover exactly the cases the Python tests cover. Minimum set (mirror the Python test files):

```go
func TestCycleIs36CLMajor(t *testing.T){ /* len 36; cycle[0]={lighthouse,lighthouse}; cycle[6]={lodestar,lighthouse}; cycle[35]={grandine,vouch} */ }
func TestNamesAndEnclave(t *testing.T){ /* combo{teku,prysm}.name()=="teku-prysm"; clusterName()=="kurtosis-teku-prysm"; enclaveName(3,...) =="c3-teku-prysm" */ }
func TestStateRoundTripAndAdvanceWrap(t *testing.T){ /* save/load tmp; advance at 34->35, 35->wrap (cycle+1,idx0) */ }
func TestSelectNextCombo(t *testing.T){ /* no override -> cycle[idx]; override -> that combo, origin "override", idx unchanged */ }
func TestComputeBackoff(t *testing.T){ /* (0,30,900)=30 (1)=60 (2)=120 (20)=900 cap */ }
func TestBuildArgsFile(t *testing.T){ /* contains "charon_node_count: 4"; token substituted, no "$PROMETHEUS_..."; nimbus vc -> json_requests; teku pin present */ }
func TestPromQLBuilders(t *testing.T){ /* metric names, cluster_name selector, [Ns], "by (duty, cluster_peer)", health_firing_now ends "== 1" */ }
func TestSelectWorstNode(t *testing.T){ /* worst = min total success; missing-success duty -> success 0, pct 0; empty -> ok=false */ }
func TestMaxValue(t *testing.T){ /* max; empty -> ok=false */ }
func TestParseHealth(t *testing.T){ /* fired+firingNow merge */ }
func TestParseCPULineAndPercent(t *testing.T){ /* "cpu  100 0 100 800 0 0..." -> busy 200 total 1000; cpuPercent (300,1000),(450,1200)=75 */ }
func TestParseMeminfo(t *testing.T){ /* MemTotal 1000kB MemAvailable 250kB -> used 750*1024 total 1000*1024 */ }
func TestLoadConfig(t *testing.T){ /* set env, required present -> defaults applied; missing required -> error */ }
func TestBuildBlocksStatuses(t *testing.T){ /* ok/degraded/failed; failed surfaces errMsg; nil optionals don't panic */ }
func TestLoadImages(t *testing.T){ /* write a temp images.json, loadImages returns pins */ }
```

- [ ] **Step 2: Run, expect failures** — `cd charon-cycler && go test ./...` → FAIL (undeclared functions).

- [ ] **Step 3: Implement the functions in `main.go`.** Port each from its Python source (cited above), matching behaviour. Add the func-var seam declarations too (used next task):

```go
var (
    runCommand = func(name string, args ...string) (string, error) { /* exec.Command, combined stdout, err */ return "", nil }
    httpGet    = func(url string) ([]byte, int, error) { /* net/http GET */ return nil, 0, nil }
    httpPost   = func(url string, body []byte) (int, error) { /* net/http POST application/json */ return 0, nil }
    nowFn      = time.Now
    sleepFn    = time.Sleep
    readFileFn = os.ReadFile
)
```
(Real bodies can be filled here or in Task 3; either way keep them tiny.)

- [ ] **Step 4: Run tests + vet** — `cd charon-cycler && go test ./... && go vet ./...` → PASS/clean.

- [ ] **Step 5: Commit**

```bash
git add charon-cycler/main.go charon-cycler/main_test.go
git commit -m "Port cycler pure logic to Go (combos, state, metrics, report, args-file, config)"
```

---

## Task 3: Go I/O glue, run loop, and `main` (+ func-var-fake tests)

**Files:**
- Extend: `charon-cycler/main.go`
- Extend: `charon-cycler/main_test.go`

**Interfaces (produced):**

```go
// I/O helpers (thin wrappers over the func vars)
func promQuery(baseURL, promql string) ([]sample, error)  // GET /api/v1/query; error if status != "success"
func slackPost(webhookURL, text string, blocks []map[string]any) error // via httpPost; err on non-200
func kurtosisRun(enclave, pkg, argsFile string) error     // runCommand("kurtosis","run",...); err on failure
func kurtosisRemove(enclave string)                        // best-effort; never returns/panics
func prometheusBaseURL(enclave string) string              // parse "kurtosis port print <e> prometheus http"; "" on error
func gitPull(repoPath string) error
func sampleHost(stopCh <-chan struct{}, intervalS int) hostStats // goroutine loop reading /proc via readFileFn; NOT interfaces — a plain func + channel
func waitHealthy(baseURL, clusterName string, deadlineS int) bool // polls core_scheduler_validators_active>0
func collectReport(baseURL string, c combo, cycle, windowS int, host hostStats, im images) (reportData, error)
func runOne(cfg config, c combo, cycle int) reportData // never panics; always tears down (defer)
func mainLoop(cfg config) // for-ever; resume; select; runOne; advance on "cycle"; backoff
```

- [ ] **Step 1: Write failing tests** using the func-var fakes (no interfaces):

```go
func TestPromQueryParsesAndErrors(t *testing.T){
    old := httpGet; defer func(){ httpGet = old }()
    httpGet = func(string)([]byte,int,error){ return []byte(`{"status":"success","data":{"result":[{"metric":{"cluster_peer":"0","duty":"attester"},"value":[1,"42.5"]}]}}`),200,nil }
    // -> one sample value 42.5
    httpGet = func(string)([]byte,int,error){ return []byte(`{"status":"error","errorType":"bad_data"}`),200,nil }
    // -> promQuery returns error
}
func TestSlackPostPayloadAndNon200(t *testing.T){ /* capture body via httpPost fake: {"text":..,"blocks":..}; non-200 -> error */ }
func TestKurtosisRunAndRemove(t *testing.T){ /* runCommand fake captures argv ["kurtosis","run","--enclave",e,pkg,"--args-file",f]; remove never panics even if fake errors */ }
func TestPrometheusBaseURLParse(t *testing.T){ /* fake stdout "http://127.0.0.1:53455\n" -> that; rc!=0 -> "" */ }
func TestRunOnePreLaunchFailurePostsFailed(t *testing.T){ /* gitPull fake errors -> runOne returns status "failed", slackPost called once, no panic */ }
func TestRunOneMidRunFailureTearsDownAndPosts(t *testing.T){ /* healthy true, promQuery errors during collect -> status "failed", kurtosisRemove called (capture), slackPost once */ }
func TestRunOneHappyPathOK(t *testing.T){ /* fakes: run ok, healthy, prom returns full duty data 780/780, mem/cpu; sleepFn no-op; nowFn fixed -> status "ok", window label == end-windowS..end */ }
func TestDegradedTolerance(t *testing.T){ /* worst duty 99.9% + no firing health -> "ok"; 95% -> "degraded" */ }
```

- [ ] **Step 2: Run, expect failures** — `cd charon-cycler && go test ./...` → FAIL.

- [ ] **Step 3: Implement the I/O helpers, `collectReport`, `runOne`, `mainLoop`, and `main()`.** Port from `metrics.py` (PrometheusClient.query → `promQuery`, error on non-success), `slack.py`, `kurtosis.py`, `cycler.py` (`collect_report`, `run_one`, `main`, `_default_deps`, `wait_healthy`). Go specifics:
  - `runOne`: mirror the Python control flow. Use `defer kurtosisRemove(enclave)` after a successful launch so teardown always runs; a guarded pre-clear `kurtosisRemove(enclave)` at the top; git/args failures → build a `failed` reportData, `slackPost` best-effort, `return`. Any error from waitHealthy/sampling/collect/slack → `failed` report, best-effort post, return (never panic). Match the Python exactly.
  - host sampling: run `sampleHost` in a goroutine started before the wait window; signal stop via a channel after `runMinutes`; collect avg/peak. Guard `sampleIntervalS>=1` via `max(1,...)`. No interfaces — a plain function reading `readFileFn("/proc/stat")` / `readFileFn("/proc/meminfo")`.
  - `mainLoop`: load state; if `CurrentEnclave != ""` best-effort remove + clear + save; loop: `selectNextCombo`; set+save `CurrentEnclave`; `runOne`; clear+save; if origin=="cycle" `advance()`+save; track `consecutiveFailures` (from returned status) and `sleepFn(computeBackoff(...))`.
  - `promQuery` uses `httpGet` on `baseURL + "/api/v1/query?query=" + url.QueryEscape(promql)`; JSON-decode; if `status != "success"` return an error including `errorType`.
  - `window := runMinutes*60 - warmupMinutes*60` (min 1). Window label from `end-window … end`.
  - `main()` = `cfg,err := loadConfig(); if err fatal; mainLoop(cfg)`.

- [ ] **Step 4: Run the full gate** — `cd charon-cycler && go vet ./... && go test ./... && go build ./...` → clean/green/compiles.

- [ ] **Step 5: Commit**

```bash
git add charon-cycler/main.go charon-cycler/main_test.go
git commit -m "Port cycler I/O, run loop, and main to Go with guaranteed teardown"
```

---

## Task 4: systemd unit, README, delete the Python cycler, final verify

**Files:**
- Modify: `charon-cycler/cycler.service`
- Modify: `charon-cycler/README.md`
- Delete: `charon-cycler/charon_cycler/`, `charon-cycler/tests/`, `charon-cycler/pyproject.toml`, `charon-cycler/config.example.yaml`

- [ ] **Step 1: Rewrite `cycler.service`** for `go run .`:

```ini
[Unit]
Description=Charon 36-combo test cycler
After=docker.service network-online.target
Wants=docker.service network-online.target

[Service]
Type=simple
User=charon
WorkingDirectory=/opt/kurtosis-charon/charon-cycler
Environment=GOCACHE=/var/cache/charon-cycler/go-build
Environment=HOME=/opt/kurtosis-charon
Environment=CYCLER_SLACK_WEBHOOK_URL=
Environment=CYCLER_REPO_PATH=/opt/kurtosis-charon
Environment=CYCLER_STATE_PATH=/var/lib/charon-cycler/state.json
Environment=CYCLER_MONITORING_TOKEN=
ExecStart=/opt/homebrew/bin/go run .
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```
(Note the operator adjusts the `go` path, `User`, and paths; `GOCACHE`/`HOME` must be writable by `User`.)

- [ ] **Step 2: Rewrite `README.md`** — build/run via `go run .`; the env-var config table (all `CYCLER_*` with defaults); the run cycle; version bumps by editing `images.json` (+ commit; cycler `git pull`s before each run); systemd install + `GOCACHE`/`HOME` note; `journalctl -u cycler -f`; state/resume; the inert `readOverride` extension point; `go test ./...` for tests.

- [ ] **Step 3: Delete the Python cycler.**
```bash
git rm -r charon-cycler/charon_cycler charon-cycler/tests charon-cycler/pyproject.toml charon-cycler/config.example.yaml
```

- [ ] **Step 4: Final verification.**
Run: `cd charon-cycler && go vet ./... && go test ./... && go build ./...` → clean/green/compiles.
Run: `grep -rn "charon_cycler\|dappnode" charon-cycler docs/superpowers/*/2026-07-29-charon-cycler-go-port* || echo clean` → no stray Python-package or dappnode refs.
Run (Python still green): `python -m pytest charon_matrix/tests -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A charon-cycler
git commit -m "Switch cycler to go run; delete Python cycler; update systemd unit and README"
```

---

## Self-Review

**Spec coverage:** script-like single `package main` (Global Constraints; Tasks 2–3 all in `main.go`) ✓; stdlib-only `go.mod` (Task 1) ✓; func-var I/O seams, no interfaces (Task 2 Step 3, Task 3 tests) ✓; `go run .` execution (Task 4) ✓; shared `images.json` read at runtime + Python refactor (Task 1, Task 2 `loadImages`, Task 3 `runOne` reads via `loadImages(repoPath)`) ✓; behaviour parity incl. worst-node, backoff, 99.5% tolerance, teardown, resume, window label, inert override (Tasks 2–3, cited to Python sources) ✓; 4 Charon nodes (Task 2 `buildArgsFile`) ✓; env/flag config (Task 2 `loadConfig`) ✓; delete Python cycler (Task 4) ✓; commits without Claude trailer (Global Constraints) ✓.

**Placeholder scan:** no TBD/TODO; each task cites the exact Python source to port and gives Go signatures + concrete test cases and commands. The "port from `X.py`" instructions reference in-repo committed code (present until Task 4), not missing content.

**Type consistency:** names/signatures used across tasks match — `combo`, `cycle`, `enclaveName`, `config`, `loadConfig`, `state`(`Cycle/NextIndex/CurrentEnclave`), `selectNextCombo`, `computeBackoff`, `images`/`loadImages`, `buildArgsFile`, `prom*` builders, `sample`/`dutyResult`/`worstNode`/`selectWorstNode`/`maxValue`/`parseHealth`, `parseCPULine`/`cpuPercent`/`parseMeminfo`, `hostStats`/`reportData`/`buildText`/`buildBlocks`, `promQuery`/`slackPost`/`kurtosis*`/`prometheusBaseURL`/`gitPull`/`sampleHost`/`waitHealthy`/`collectReport`/`runOne`/`mainLoop`, and the func vars `runCommand`/`httpGet`/`httpPost`/`nowFn`/`sleepFn`/`readFileFn`. Consistent throughout.
