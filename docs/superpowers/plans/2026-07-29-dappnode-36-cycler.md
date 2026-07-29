# Dappnode 24/7 Sequential 36-Combo Cycler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Python service that runs the full 6×6 CL/VC Charon matrix sequentially on the dappnode, 90 minutes per combo, 24/7, posting a per-run results report to Slack.

**Architecture:** A long-running loop (systemd) picks the next combo, `git pull`s, launches the native `ethereum-package@charon` harness via Kurtosis with 4 Charon nodes, waits 90 min while sampling host resources, queries the in-enclave Prometheus, posts a Slack Block Kit report, tears the enclave down, persists state, and repeats. Pure logic (combo ordering, worst-node selection, ratio math, report building, state) is isolated from thin I/O adapters (Kurtosis CLI, Prometheus HTTP, Slack webhook, `/proc`) so it is unit-testable.

**Tech Stack:** Python 3 (stdlib only: `urllib`, `subprocess`, `json`, `dataclasses`, `threading`; `pytest` for tests; `PyYAML` for config). Kurtosis CLI, Prometheus, Docker on the host.

## Global Constraints

- **Language:** Python 3, standard library only for runtime except `PyYAML` (config). No `requests`, no `boto3`, no `tabulate` in the cycler.
- **Harness:** `github.com/ObolNetwork/ethereum-package@charon`, used unmodified.
- **Charon image under test:** `obolnetwork/charon:next` (moving tag, do not pin).
- **Cluster size:** `charon_node_count: 4`.
- **Matrix:** `CLS = [lighthouse, lodestar, nimbus, teku, prysm, grandine]`, `VCS = [lighthouse, lodestar, nimbus, teku, prysm, vouch]`, CL-major order, 36 combos.
- **Cluster label:** `cluster_name = "kurtosis-<cl>-<vc>"` (harness sets this via `--name`).
- **Run window:** 90 min wall-clock; duty ratios over `[genesis + warmup, end]`, warmup default 15 min.
- **Duty reporting:** worst Charon node = `cluster_peer` with fewest total successful duties.
- **Charon CPU/mem:** max across the 4 nodes (worst case), per-metric.
- **Machine total:** whole host, sampled from `/proc`.
- **Failure policy:** report and continue; never halt the cycle.
- **Slack:** Incoming Webhook, one message per run.
- **Version updates:** `git pull --ff-only` before every run; pins live in `charon_matrix/network_params.py`.
- **Repo layout:** shared generator in `charon_matrix/` (repo root, importable package); cycler in `dappnode-cycler/`.
- **Commit signing:** commits use the repo's configured YubiKey signing; retry once if the first attempt reports a signing format error.

---

## File Structure

```
kurtosis-charon/
  charon_matrix/                      # NEW shared package (dependency-free)
    __init__.py
    network_params.py                 # build_network_params + image pins + CLS/VCS (moved from AWS runner)
  kurtosis-aws-runner/
    kurtosis_aws_runner_native.py     # MODIFIED: import shared constants instead of defining inline
  dappnode-cycler/                    # NEW cycler
    pyproject.toml                    # pytest pythonpath config
    README.md
    config.example.yaml
    cycler.service                    # systemd unit
    dappnode_cycler/
      __init__.py
      combos.py                       # Combo, CYCLE, enclave_name
      state.py                        # State (persist/resume)
      selection.py                    # select_next_combo + override hook (stub)
      params.py                       # build args-file (charon_node_count=4, token subst)
      promql.py                       # pure PromQL string builders
      metrics.py                      # PrometheusClient + Sample + worst-node/resource/health parsing
      host_sampler.py                 # /proc CPU%/mem sampling + Sampler thread
      report.py                       # ReportData + Slack Block Kit builder
      slack.py                        # webhook POST
      kurtosis.py                     # Kurtosis CLI wrappers
      config.py                       # Config loader
      cycler.py                       # run_one + main loop wiring
    tests/
      test_combos.py test_state.py test_selection.py test_params.py
      test_promql.py test_metrics.py test_host_sampler.py test_report.py
      test_slack.py test_kurtosis.py test_config.py test_cycler.py
```

Run tests from `dappnode-cycler/` (its `pyproject.toml` puts `.` and `..` on `pythonpath`, so both `dappnode_cycler` and `charon_matrix` import).

---

## Task 1: Extract shared network-params generator

**Files:**
- Create: `charon_matrix/__init__.py` (empty)
- Create: `charon_matrix/network_params.py`
- Create: `charon_matrix/tests/test_network_params.py`
- Modify: `kurtosis-aws-runner/kurtosis_aws_runner_native.py:22-74` (replace inline constants/generator with an import)

**Interfaces:**
- Produces: `CLS: list[str]`, `VCS: list[str]`, `CHARON_IMAGE: str`, `EL_IMAGE: str`, `BOOTSTRAP_CL_IMAGE: str`, `CL_IMAGES: dict`, `VC_IMAGES: dict`, `VALIDATOR_KEYS_MNEMONIC: str`, and `build_network_params(cl: str, vc: str, charon_node_count: int = 3) -> str`.

- [ ] **Step 1: Write the failing test**

```python
# charon_matrix/tests/test_network_params.py
from charon_matrix.network_params import build_network_params, CLS, VCS, CL_IMAGES

def test_matrix_shape():
    assert len(CLS) == 6 and len(VCS) == 6
    assert CLS[0] == "lighthouse" and VCS[-1] == "vouch"

def test_generates_combo_yaml_with_node_count():
    y = build_network_params("teku", "prysm", charon_node_count=4)
    assert "charon_node_count: 4" in y
    assert CL_IMAGES["teku"] in y
    assert "charon_vc: prysm" in y
    assert 'remote_write_url: "https://vm.monitoring.gcp.obol.tech/write"' in y

def test_nimbus_vc_gets_json_requests():
    assert "CHARON_FEATURE_SET_ENABLE: json_requests" in build_network_params("teku", "nimbus")
    assert "CHARON_FEATURE_SET_ENABLE" not in build_network_params("teku", "prysm")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd charon_matrix && python -m pytest tests/test_network_params.py -v` (from repo root: `python -m pytest charon_matrix/tests -v`)
Expected: FAIL with `ModuleNotFoundError: charon_matrix.network_params`.

- [ ] **Step 3: Create the shared module**

Move the constant blocks and generator out of `kurtosis-aws-runner/kurtosis_aws_runner_native.py` (lines 22–74: `CHARON_IMAGE`, `EL_IMAGE`, `BOOTSTRAP_CL_IMAGE`, `CL_IMAGES`, `VC_IMAGES`, `CLS`, `VCS`, `VALIDATOR_KEYS_MNEMONIC`) and the `build_network_params` function (lines 75–153) into `charon_matrix/network_params.py` **verbatim**, with one change: add the `charon_node_count` parameter.

```python
# charon_matrix/network_params.py
CHARON_IMAGE = "obolnetwork/charon:next"
EL_IMAGE = "ethereum/client-go:v1.17.4"
BOOTSTRAP_CL_IMAGE = "sigp/lighthouse:v8.2.1"

CL_IMAGES = {
    "lighthouse": "sigp/lighthouse:v8.2.1",
    "lodestar": "chainsafe/lodestar:v1.45.0",
    "nimbus": "statusim/nimbus-eth2:multiarch-v26.7.0",
    "teku": "consensys/teku:26.7.1",
    "prysm": "gcr.io/prysmaticlabs/prysm/beacon-chain:v7.1.8",
    "grandine": "sifrai/grandine:2.0.5",
}
VC_IMAGES = {
    "lighthouse": "sigp/lighthouse:v8.2.1",
    "lodestar": "chainsafe/lodestar:v1.45.0",
    "nimbus": "statusim/nimbus-validator-client:multiarch-v26.7.0",
    "teku": "consensys/teku:26.7.1",
    "prysm": "gcr.io/prysmaticlabs/prysm/validator:v7.1.8",
    "vouch": "attestant/vouch:1.13.1",
}
CLS = ["lighthouse", "lodestar", "nimbus", "teku", "prysm", "grandine"]
VCS = ["lighthouse", "lodestar", "nimbus", "teku", "prysm", "vouch"]
VALIDATOR_KEYS_MNEMONIC = (
    "giant issue aisle success illegal bike spike question tent bar rely arctic "
    "volcano long crawl hungry vocal artwork sniff fantasy very lucky have athlete"
)


def build_network_params(cl, vc, charon_node_count=3):
    """Return the full ethereum-package args-file YAML for one CL x VC combo.

    Bootstrap 2x geth/lighthouse supernode keeps the chain alive; participant 1 is
    the combo under test (<cl> beacon / charon:next / <vc>). The
    $PROMETHEUS_REMOTE_WRITE_TOKEN placeholder is left intact for later substitution.
    """
    nimbus_env = ""
    if vc == "nimbus":
        nimbus_env = (
            "    vc_extra_env_vars:\n"
            "      CHARON_FEATURE_SET_ENABLE: json_requests\n"
        )
    return f"""participants:
  - el_type: geth
    el_image: {EL_IMAGE}
    cl_type: lighthouse
    cl_image: {BOOTSTRAP_CL_IMAGE}
    use_separate_vc: true
    vc_type: lighthouse
    vc_image: {BOOTSTRAP_CL_IMAGE}
    count: 2
    supernode: true

  - el_type: geth
    el_image: {EL_IMAGE}
    cl_type: {cl}
    cl_image: {CL_IMAGES[cl]}
    supernode: true
    use_separate_vc: true
    vc_type: charon
    vc_image: {CHARON_IMAGE}
    charon_node_count: {charon_node_count}
    charon_params:
      charon_vc: {vc}
      charon_vc_image: {VC_IMAGES[vc]}
{nimbus_env}    count: 1
network_params:
  network: kurtosis
  network_id: "3151908"
  deposit_contract_address: "0x4242424242424242424242424242424242424242"
  seconds_per_slot: 12
  num_validator_keys_per_node: 128
  preregistered_validator_keys_mnemonic: "{VALIDATOR_KEYS_MNEMONIC}"
  shard_committee_period: 1
  prefunded_accounts: '{{"0xb9e79D19f651a941757b35830232E7EFC77E1c79": {{"balance": "100000ETH"}}}}'
wait_for_finalization: false
global_log_level: info
parallel_keystore_generation: false
mev_type: flashbots
mev_params:
  mev_builder_subsidy: 1
prometheus_params:
  storage_tsdb_retention_time: 3h
  remote_write_url: "https://vm.monitoring.gcp.obol.tech/write"
  remote_write_token: "$PROMETHEUS_REMOTE_WRITE_TOKEN"
  remote_write_relabel_configs:
    - SourceLabels: ["job"]
      Regex: ".*charon.*"
      Action: keep
    - SourceLabels: ["client_name"]
      Regex: "charon"
      TargetLabel: job
      Replacement: charon
      Action: replace
additional_services:
  - spamoor
  - prometheus
"""
```

Create empty `charon_matrix/__init__.py`. Add `charon_matrix/tests/__init__.py` (empty) if pytest needs it.

- [ ] **Step 4: Point the AWS runner at the shared module**

In `kurtosis-aws-runner/kurtosis_aws_runner_native.py`, delete the moved definitions and add near the top (after `import sys`):

```python
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from charon_matrix.network_params import (  # noqa: E402
    CHARON_IMAGE, EL_IMAGE, BOOTSTRAP_CL_IMAGE, CL_IMAGES, VC_IMAGES,
    CLS, VCS, VALIDATOR_KEYS_MNEMONIC, build_network_params,
)
```

Keep the AWS-only `COMBOS`, `enclave_name`, etc. in place (they still reference `CLS`/`VCS`).

- [ ] **Step 5: Run tests to verify they pass and the AWS runner still imports**

Run: `python -m pytest charon_matrix/tests -v`
Expected: PASS.
Run: `python -c "import sys; sys.argv=['x','--dump-configs','/tmp/cfgcheck']; exec(open('kurtosis-aws-runner/kurtosis_aws_runner_native.py').read())" || true` then confirm no `ImportError`/`NameError` (a clean `--dump-configs` run or an argparse/AWS error is fine; an import/name error is not).

- [ ] **Step 6: Commit**

```bash
git add charon_matrix kurtosis-aws-runner/kurtosis_aws_runner_native.py
git commit -m "Extract shared network-params generator into charon_matrix"
```

---

## Task 2: Combos, ordering, and enclave naming

**Files:**
- Create: `dappnode-cycler/pyproject.toml`
- Create: `dappnode-cycler/dappnode_cycler/__init__.py` (empty)
- Create: `dappnode-cycler/dappnode_cycler/combos.py`
- Create: `dappnode-cycler/tests/test_combos.py`

**Interfaces:**
- Consumes: `CLS`, `VCS` from `charon_matrix.network_params`.
- Produces: `@dataclass Combo` with fields `cl: str`, `vc: str`; properties `name -> "<cl>-<vc>"`, `cluster_name -> "kurtosis-<cl>-<vc>"`. Module constant `CYCLE: list[Combo]` (36, CL-major). `enclave_name(combo: Combo, cycle: int) -> str` → `"c{cycle}-{cl}-{vc}"`.

- [ ] **Step 1: Create pyproject for test paths**

```toml
# dappnode-cycler/pyproject.toml
[tool.pytest.ini_options]
pythonpath = [".", ".."]
testpaths = ["tests"]
```

- [ ] **Step 2: Write the failing test**

```python
# dappnode-cycler/tests/test_combos.py
from dappnode_cycler.combos import Combo, CYCLE, enclave_name

def test_cycle_is_36_cl_major():
    assert len(CYCLE) == 36
    assert CYCLE[0] == Combo("lighthouse", "lighthouse")
    assert CYCLE[6] == Combo("lodestar", "lighthouse")  # CL-major: 7th entry rolls CL
    assert CYCLE[-1] == Combo("grandine", "vouch")

def test_names():
    c = Combo("teku", "prysm")
    assert c.name == "teku-prysm"
    assert c.cluster_name == "kurtosis-teku-prysm"
    assert enclave_name(c, 3) == "c3-teku-prysm"
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_combos.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 4: Implement**

```python
# dappnode-cycler/dappnode_cycler/combos.py
from dataclasses import dataclass
from charon_matrix.network_params import CLS, VCS


@dataclass(frozen=True)
class Combo:
    cl: str
    vc: str

    @property
    def name(self) -> str:
        return f"{self.cl}-{self.vc}"

    @property
    def cluster_name(self) -> str:
        return f"kurtosis-{self.cl}-{self.vc}"


CYCLE = [Combo(cl, vc) for cl in CLS for vc in VCS]


def enclave_name(combo: Combo, cycle: int) -> str:
    return f"c{cycle}-{combo.cl}-{combo.vc}"
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_combos.py -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add dappnode-cycler/pyproject.toml dappnode-cycler/dappnode_cycler dappnode-cycler/tests/test_combos.py
git commit -m "Add cycler combo matrix, ordering, enclave naming"
```

---

## Task 3: State persistence and resume

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/state.py`
- Create: `dappnode-cycler/tests/test_state.py`

**Interfaces:**
- Produces: `@dataclass State` with `cycle: int = 0`, `next_index: int = 0`, `current_enclave: str | None = None`. Methods: `advance() -> None` (increment `next_index`; when it reaches `len(CYCLE)` wrap to 0 and `cycle += 1`), `save(path: str) -> None` (atomic), classmethod `load(path: str) -> State` (returns default `State()` if file missing).

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_state.py
from dappnode_cycler.state import State

def test_defaults_and_roundtrip(tmp_path):
    p = str(tmp_path / "state.json")
    assert State.load(p) == State(0, 0, None)     # missing file -> defaults
    s = State(cycle=2, next_index=35, current_enclave="c2-grandine-vouch")
    s.save(p)
    assert State.load(p) == s

def test_advance_wraps_and_bumps_cycle():
    s = State(cycle=0, next_index=34)
    s.advance()
    assert (s.cycle, s.next_index) == (0, 35)
    s.advance()
    assert (s.cycle, s.next_index) == (1, 0)   # wrapped past 36 combos

def test_save_is_atomic(tmp_path):
    p = str(tmp_path / "state.json")
    State(1, 5, None).save(p)
    import os
    assert not any(f.endswith(".tmp") for f in os.listdir(tmp_path))  # no temp left behind
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_state.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/state.py
import json
import os
from dataclasses import dataclass, asdict
from dappnode_cycler.combos import CYCLE


@dataclass
class State:
    cycle: int = 0
    next_index: int = 0
    current_enclave: str | None = None

    def advance(self) -> None:
        self.next_index += 1
        if self.next_index >= len(CYCLE):
            self.next_index = 0
            self.cycle += 1

    def save(self, path: str) -> None:
        tmp = path + ".tmp"
        with open(tmp, "w") as f:
            json.dump(asdict(self), f, indent=2)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, path)

    @classmethod
    def load(cls, path: str) -> "State":
        if not os.path.exists(path):
            return cls()
        with open(path) as f:
            return cls(**json.load(f))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_state.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/state.py dappnode-cycler/tests/test_state.py
git commit -m "Add cycler state persistence with atomic save and resume"
```

---

## Task 4: Combo selection with priority-override hook

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/selection.py`
- Create: `dappnode-cycler/tests/test_selection.py`

**Interfaces:**
- Consumes: `Combo`, `CYCLE` (Task 2); `State` (Task 3).
- Produces: `read_override() -> dict | None` (stub, returns `None` — the designed-in extension point), `combo_from_override(ov: dict) -> Combo`, and `select_next_combo(state: State, override_reader=read_override) -> tuple[Combo, str]` where the second element is the origin `"override"` or `"cycle"`. An override run must NOT advance `state.next_index`.

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_selection.py
from dappnode_cycler.combos import Combo
from dappnode_cycler.state import State
from dappnode_cycler.selection import select_next_combo, read_override

def test_default_reader_returns_none():
    assert read_override() is None

def test_selects_from_cycle_when_no_override():
    combo, origin = select_next_combo(State(next_index=6), override_reader=lambda: None)
    assert origin == "cycle"
    assert combo == Combo("lodestar", "lighthouse")

def test_override_takes_priority_without_advancing():
    st = State(next_index=6)
    combo, origin = select_next_combo(st, override_reader=lambda: {"cl": "prysm", "vc": "teku"})
    assert origin == "override"
    assert combo == Combo("prysm", "teku")
    assert st.next_index == 6  # cycle position untouched
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_selection.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/selection.py
from dappnode_cycler.combos import Combo, CYCLE
from dappnode_cycler.state import State


def read_override():
    """Extension point (not built yet): return {"cl","vc"[,"sticky"]} or None.

    A future implementation reads dappnode-cycler/override.json. Until then this
    stub returns None so the cycle runs normally, while select_next_combo already
    handles the override branch.
    """
    return None


def combo_from_override(ov: dict) -> Combo:
    return Combo(ov["cl"], ov["vc"])


def select_next_combo(state: State, override_reader=read_override):
    ov = override_reader()
    if ov:
        return combo_from_override(ov), "override"
    return CYCLE[state.next_index], "cycle"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_selection.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/selection.py dappnode-cycler/tests/test_selection.py
git commit -m "Add combo selection with designed-in override hook"
```

---

## Task 5: Args-file builder (4 nodes + token substitution)

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/params.py`
- Create: `dappnode-cycler/tests/test_params.py`

**Interfaces:**
- Consumes: `build_network_params` (Task 1); `Combo` (Task 2).
- Produces: `build_args_file(combo: Combo, token: str, charon_node_count: int = 4) -> str` — the generated YAML with `charon_node_count` applied and `$PROMETHEUS_REMOTE_WRITE_TOKEN` replaced by `token`. `write_args_file(combo, token, dir_path, charon_node_count=4) -> str` writes it to `<dir>/network_params.yaml` and returns the path.

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_params.py
from dappnode_cycler.combos import Combo
from dappnode_cycler.params import build_args_file, write_args_file

def test_four_nodes_and_token_substituted():
    y = build_args_file(Combo("lighthouse", "teku"), token="SECRET123")
    assert "charon_node_count: 4" in y
    assert "$PROMETHEUS_REMOTE_WRITE_TOKEN" not in y
    assert "SECRET123" in y

def test_write_args_file(tmp_path):
    path = write_args_file(Combo("prysm", "vouch"), "tok", str(tmp_path))
    assert path.endswith("network_params.yaml")
    assert "charon_vc: vouch" in open(path).read()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_params.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/params.py
import os
from charon_matrix.network_params import build_network_params
from dappnode_cycler.combos import Combo


def build_args_file(combo: Combo, token: str, charon_node_count: int = 4) -> str:
    raw = build_network_params(combo.cl, combo.vc, charon_node_count=charon_node_count)
    return raw.replace("$PROMETHEUS_REMOTE_WRITE_TOKEN", token)


def write_args_file(combo: Combo, token: str, dir_path: str, charon_node_count: int = 4) -> str:
    path = os.path.join(dir_path, "network_params.yaml")
    with open(path, "w") as f:
        f.write(build_args_file(combo, token, charon_node_count))
    return path
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_params.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/params.py dappnode-cycler/tests/test_params.py
git commit -m "Add dappnode args-file builder (4 nodes, token subst)"
```

---

## Task 6: PromQL query builders

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/promql.py`
- Create: `dappnode-cycler/tests/test_promql.py`

**Interfaces:**
- Produces (all take `cluster_name: str`, and where noted `window_s: int`; return `str`):
  - `duty_expected(cluster_name, window_s) -> str`
  - `duty_success(cluster_name, window_s) -> str`
  - `charon_mem_peak(cluster_name, window_s) -> str`
  - `charon_cpu_peak(cluster_name, window_s) -> str`
  - `health_fired(cluster_name, window_s) -> str`
  - `health_firing_now(cluster_name) -> str`

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_promql.py
from dappnode_cycler import promql

def test_duty_queries_group_by_peer_and_use_window():
    q = promql.duty_success("kurtosis-teku-prysm", 4500)
    assert "core_tracker_success_duties_total" in q
    assert 'cluster_name="kurtosis-teku-prysm"' in q
    assert "[4500s]" in q
    assert "by (duty, cluster_peer)" in q

def test_expected_uses_expect_metric():
    assert "core_tracker_expect_duties_total" in promql.duty_expected("kurtosis-a-b", 60)

def test_resource_and_health_queries():
    assert "process_resident_memory_bytes" in promql.charon_mem_peak("kurtosis-a-b", 5400)
    assert "process_cpu_seconds_total" in promql.charon_cpu_peak("kurtosis-a-b", 5400)
    assert "max_over_time(app_health_checks" in promql.health_fired("kurtosis-a-b", 5400)
    assert promql.health_firing_now("kurtosis-a-b").strip().endswith("== 1")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_promql.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/promql.py
def _sel(cluster_name: str) -> str:
    return f'cluster_name="{cluster_name}"'


def duty_expected(cluster_name: str, window_s: int) -> str:
    return (f"sum(increase(core_tracker_expect_duties_total{{{_sel(cluster_name)}}}"
            f"[{window_s}s])) by (duty, cluster_peer)")


def duty_success(cluster_name: str, window_s: int) -> str:
    return (f"sum(increase(core_tracker_success_duties_total{{{_sel(cluster_name)}}}"
            f"[{window_s}s])) by (duty, cluster_peer)")


def charon_mem_peak(cluster_name: str, window_s: int) -> str:
    return (f"max(max_over_time(process_resident_memory_bytes{{{_sel(cluster_name)}}}"
            f"[{window_s}s])) by (cluster_peer)")


def charon_cpu_peak(cluster_name: str, window_s: int) -> str:
    return (f"max(max_over_time(rate(process_cpu_seconds_total{{{_sel(cluster_name)}}}"
            f"[1m])[{window_s}s:1m])) by (cluster_peer)")


def health_fired(cluster_name: str, window_s: int) -> str:
    return (f"max_over_time(app_health_checks{{{_sel(cluster_name)}}}"
            f"[{window_s}s]) > 0")


def health_firing_now(cluster_name: str) -> str:
    return f"app_health_checks{{{_sel(cluster_name)}}} == 1"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_promql.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/promql.py dappnode-cycler/tests/test_promql.py
git commit -m "Add PromQL builders for duties, resources, health checks"
```

---

## Task 7: Prometheus client + metrics aggregation

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/metrics.py`
- Create: `dappnode-cycler/tests/test_metrics.py`

**Interfaces:**
- Consumes: `promql` (Task 6).
- Produces:
  - `@dataclass Sample`: `labels: dict[str, str]`, `value: float`.
  - `@dataclass DutyResult`: `duty: str`, `expected: float`, `success: float`; property `pct -> float` (0.0 if expected == 0).
  - `@dataclass WorstNode`: `peer: str`, `duties: list[DutyResult]`.
  - `@dataclass HealthCheck`: `name: str`, `severity: str`, `firing_now: bool`.
  - `select_worst_node(expected: list[Sample], success: list[Sample]) -> WorstNode | None`.
  - `max_value(samples: list[Sample]) -> float | None`.
  - `parse_health(fired: list[Sample], firing_now: list[Sample]) -> list[HealthCheck]`.
  - `class PrometheusClient(base_url)`: `query(promql: str) -> list[Sample]` (instant query via `/api/v1/query`), using an overridable `_http_get(url) -> str` for testing.

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_metrics.py
import json
from dappnode_cycler.metrics import (
    Sample, DutyResult, select_worst_node, max_value, parse_health, PrometheusClient,
)

def S(labels, value):
    return Sample(labels, value)

def test_worst_node_is_min_total_success():
    expected = [S({"duty": "attester", "cluster_peer": "0"}, 100),
                S({"duty": "attester", "cluster_peer": "1"}, 100),
                S({"duty": "aggregator", "cluster_peer": "0"}, 20),
                S({"duty": "aggregator", "cluster_peer": "1"}, 20)]
    success = [S({"duty": "attester", "cluster_peer": "0"}, 100),
               S({"duty": "attester", "cluster_peer": "1"}, 90),
               S({"duty": "aggregator", "cluster_peer": "0"}, 20),
               S({"duty": "aggregator", "cluster_peer": "1"}, 15)]
    wn = select_worst_node(expected, success)
    assert wn.peer == "1"  # 105 total success < 120
    by_duty = {d.duty: d for d in wn.duties}
    assert by_duty["aggregator"].expected == 20 and by_duty["aggregator"].success == 15
    assert round(by_duty["aggregator"].pct, 2) == 75.0

def test_pct_zero_expected():
    assert DutyResult("proposer", 0, 0).pct == 0.0

def test_max_value():
    assert max_value([S({"cluster_peer": "0"}, 3.0), S({"cluster_peer": "1"}, 7.5)]) == 7.5
    assert max_value([]) is None

def test_parse_health_merges_firing_now():
    fired = [S({"name": "high-mem", "severity": "warning"}, 1),
             S({"name": "peer-count", "severity": "error"}, 1)]
    now = [S({"name": "peer-count", "severity": "error"}, 1)]
    checks = {(c.name, c.severity): c for c in parse_health(fired, now)}
    assert checks[("high-mem", "warning")].firing_now is False
    assert checks[("peer-count", "error")].firing_now is True

def test_prometheus_client_parses_result():
    body = json.dumps({"status": "success", "data": {"resultType": "vector", "result": [
        {"metric": {"duty": "attester", "cluster_peer": "0"}, "value": [123, "42.5"]}]}})
    c = PrometheusClient("http://x:9090")
    c._http_get = lambda url: body
    out = c.query("whatever")
    assert out == [Sample({"duty": "attester", "cluster_peer": "0"}, 42.5)]
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_metrics.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/metrics.py
import json
import urllib.parse
import urllib.request
from dataclasses import dataclass, field


@dataclass
class Sample:
    labels: dict
    value: float


@dataclass
class DutyResult:
    duty: str
    expected: float
    success: float

    @property
    def pct(self) -> float:
        return 0.0 if self.expected == 0 else 100.0 * self.success / self.expected


@dataclass
class WorstNode:
    peer: str
    duties: list = field(default_factory=list)


@dataclass
class HealthCheck:
    name: str
    severity: str
    firing_now: bool


def _by_peer_duty(samples):
    out = {}
    for s in samples:
        out[(s.labels.get("cluster_peer"), s.labels.get("duty"))] = s.value
    return out


def select_worst_node(expected, success):
    peers = {s.labels.get("cluster_peer") for s in expected + success}
    peers.discard(None)
    if not peers:
        return None
    exp = _by_peer_duty(expected)
    suc = _by_peer_duty(success)
    total_success = {p: sum(v for (pp, _), v in suc.items() if pp == p) for p in peers}
    worst = min(peers, key=lambda p: (total_success[p], p))
    duties = {}
    for (p, duty), v in exp.items():
        if p == worst and duty is not None:
            duties.setdefault(duty, [0.0, 0.0])[0] = v
    for (p, duty), v in suc.items():
        if p == worst and duty is not None:
            duties.setdefault(duty, [0.0, 0.0])[1] = v
    results = [DutyResult(d, e, s) for d, (e, s) in sorted(duties.items())]
    return WorstNode(worst, results)


def max_value(samples):
    if not samples:
        return None
    return max(s.value for s in samples)


def parse_health(fired, firing_now):
    now = {(s.labels.get("name"), s.labels.get("severity")) for s in firing_now}
    checks = []
    for s in fired:
        key = (s.labels.get("name"), s.labels.get("severity"))
        checks.append(HealthCheck(key[0], key[1], key in now))
    return checks


class PrometheusClient:
    def __init__(self, base_url: str):
        self.base_url = base_url.rstrip("/")

    def _http_get(self, url: str) -> str:
        with urllib.request.urlopen(url, timeout=30) as r:
            return r.read().decode()

    def query(self, promql: str):
        url = f"{self.base_url}/api/v1/query?" + urllib.parse.urlencode({"query": promql})
        payload = json.loads(self._http_get(url))
        result = payload.get("data", {}).get("result", [])
        return [Sample(item["metric"], float(item["value"][1])) for item in result]
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_metrics.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/metrics.py dappnode-cycler/tests/test_metrics.py
git commit -m "Add Prometheus client and worst-node/resource/health aggregation"
```

---

## Task 8: Host resource sampler

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/host_sampler.py`
- Create: `dappnode-cycler/tests/test_host_sampler.py`

**Interfaces:**
- Produces:
  - `@dataclass HostStats`: `cpu_avg: float`, `cpu_peak: float`, `mem_avg: float`, `mem_peak: float`, `mem_total: float`.
  - `parse_cpu_line(text: str) -> tuple[float, float]` → `(busy, total)` jiffies from a `/proc/stat` `cpu ...` line.
  - `cpu_percent(prev: tuple, cur: tuple) -> float` → 0–100 over the interval.
  - `parse_meminfo(text: str) -> tuple[float, float]` → `(used_bytes, total_bytes)`.
  - `class Sampler(interval_s=15)`: `start()`, `stop()`, `summary() -> HostStats`; reads `/proc/stat` + `/proc/meminfo` via overridable `_read_stat()` / `_read_meminfo()`.

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_host_sampler.py
from dappnode_cycler.host_sampler import (
    parse_cpu_line, cpu_percent, parse_meminfo, Sampler, HostStats,
)

STAT = "cpu  100 0 100 800 0 0 0 0 0 0\n"

def test_parse_cpu_line():
    busy, total = parse_cpu_line(STAT)
    assert (busy, total) == (300, 1000)  # busy = total(1000) - idle(700 = 800-... )? see impl

def test_cpu_percent_over_interval():
    prev = (300, 1000)
    cur = (450, 1200)   # +150 busy of +200 total -> 75%
    assert cpu_percent(prev, cur) == 75.0

def test_parse_meminfo():
    text = "MemTotal: 1000 kB\nMemAvailable: 250 kB\n"
    used, total = parse_meminfo(text)
    assert total == 1000 * 1024
    assert used == 750 * 1024

def test_sampler_summary_avg_and_peak():
    s = Sampler(interval_s=0)
    stats = [("cpu  100 0 100 800 0 0 0 0 0 0\n", "MemTotal: 1000 kB\nMemAvailable: 500 kB\n"),
             ("cpu  250 0 200 950 0 0 0 0 0 0\n", "MemTotal: 1000 kB\nMemAvailable: 250 kB\n")]
    it = iter(stats)
    def rd():
        s._pending = next(it)
    # feed two samples manually
    s._read_stat = lambda: s._pending[0]
    s._read_meminfo = lambda: s._pending[1]
    rd(); s._sample_once()
    rd(); s._sample_once()
    out = s.summary()
    assert isinstance(out, HostStats)
    assert out.mem_peak == 750 * 1024   # second sample used 750kB
    assert out.mem_total == 1000 * 1024
    assert 0.0 <= out.cpu_avg <= 100.0 and out.cpu_peak >= out.cpu_avg
```

Implementation note for `parse_cpu_line`: fields after `cpu` are user, nice, system, idle, iowait, irq, softirq, steal, guest, guest_nice. `total = sum(all)`, `idle = idle + iowait`, `busy = total - idle`. For `STAT` above: total = 100+0+100+800 = 1000, idle = 800+0 = 800, busy = 200. Adjust the test's expected `(busy, total)` to `(200, 1000)` when you implement — write the test to match this formula.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_host_sampler.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/host_sampler.py
import threading
import time
from dataclasses import dataclass


@dataclass
class HostStats:
    cpu_avg: float
    cpu_peak: float
    mem_avg: float
    mem_peak: float
    mem_total: float


def parse_cpu_line(text: str):
    parts = text.splitlines()[0].split()[1:]
    nums = [float(x) for x in parts]
    total = sum(nums)
    idle = nums[3] + (nums[4] if len(nums) > 4 else 0.0)  # idle + iowait
    return total - idle, total


def cpu_percent(prev, cur):
    busy = cur[0] - prev[0]
    total = cur[1] - prev[1]
    return 0.0 if total <= 0 else 100.0 * busy / total


def parse_meminfo(text: str):
    vals = {}
    for line in text.splitlines():
        k, _, rest = line.partition(":")
        vals[k.strip()] = float(rest.strip().split()[0]) * 1024  # kB -> bytes
    total = vals["MemTotal"]
    avail = vals.get("MemAvailable", 0.0)
    return total - avail, total


class Sampler:
    def __init__(self, interval_s: int = 15):
        self.interval_s = interval_s
        self._cpu_samples = []
        self._mem_samples = []
        self._mem_total = 0.0
        self._prev_cpu = None
        self._stop = threading.Event()
        self._thread = None

    def _read_stat(self) -> str:
        with open("/proc/stat") as f:
            return f.read()

    def _read_meminfo(self) -> str:
        with open("/proc/meminfo") as f:
            return f.read()

    def _sample_once(self):
        cur = parse_cpu_line(self._read_stat())
        if self._prev_cpu is not None:
            self._cpu_samples.append(cpu_percent(self._prev_cpu, cur))
        self._prev_cpu = cur
        used, total = parse_meminfo(self._read_meminfo())
        self._mem_samples.append(used)
        self._mem_total = total

    def _loop(self):
        self._sample_once()  # prime cpu baseline
        while not self._stop.wait(self.interval_s):
            self._sample_once()

    def start(self):
        self._thread = threading.Thread(target=self._loop, daemon=True)
        self._thread.start()

    def stop(self):
        self._stop.set()
        if self._thread:
            self._thread.join(timeout=5)

    def summary(self) -> HostStats:
        cpu = self._cpu_samples or [0.0]
        mem = self._mem_samples or [0.0]
        return HostStats(
            cpu_avg=sum(cpu) / len(cpu),
            cpu_peak=max(cpu),
            mem_avg=sum(mem) / len(mem),
            mem_peak=max(mem),
            mem_total=self._mem_total,
        )
```

- [ ] **Step 4: Run test to verify it passes**

Fix the `test_parse_cpu_line` expectation to `(200, 1000)` per the formula note. Run: `cd dappnode-cycler && python -m pytest tests/test_host_sampler.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/host_sampler.py dappnode-cycler/tests/test_host_sampler.py
git commit -m "Add host CPU/mem sampler (/proc, avg+peak)"
```

---

## Task 9: Report model + Slack Block Kit builder

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/report.py`
- Create: `dappnode-cycler/tests/test_report.py`

**Interfaces:**
- Consumes: `WorstNode`, `HealthCheck` (Task 7); `HostStats` (Task 8); `Combo` (Task 2).
- Produces:
  - `@dataclass ReportData`: `combo: Combo`, `cycle: int`, `status: str` (`"ok"|"degraded"|"failed"`), `cl_image: str`, `vc_image: str`, `charon_image: str`, `window: str`, `worst_node: WorstNode | None`, `charon_mem_bytes: float | None`, `charon_cpu: float | None`, `host: HostStats | None`, `health: list[HealthCheck]`, `error: str | None = None`.
  - `build_text(data: ReportData) -> str` (plain-text fallback / notification line).
  - `build_blocks(data: ReportData) -> list[dict]` (Slack Block Kit).

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_report.py
from dappnode_cycler.combos import Combo
from dappnode_cycler.metrics import WorstNode, DutyResult, HealthCheck
from dappnode_cycler.host_sampler import HostStats
from dappnode_cycler.report import ReportData, build_text, build_blocks

def _data(status="ok", health=None, worst=None):
    return ReportData(
        combo=Combo("teku", "prysm"), cycle=3, status=status,
        cl_image="consensys/teku:26.7.1", vc_image="gcr.io/.../validator:v7.1.8",
        charon_image="obolnetwork/charon:next", window="12:00-13:30 UTC",
        worst_node=worst or WorstNode("1", [DutyResult("attester", 780, 780),
                                            DutyResult("aggregator", 150, 130)]),
        charon_mem_bytes=512 * 1024 * 1024, charon_cpu=1.4,
        host=HostStats(30.0, 82.0, 8e9, 9e9, 16e9),
        health=health or [HealthCheck("high-inclusion-delay", "warning", False)],
    )

def test_text_has_combo_and_cycle():
    t = build_text(_data())
    assert "teku" in t and "prysm" in t and "cycle 3" in t.lower()

def test_blocks_render_duty_ratios_and_worst_peer():
    blocks = build_blocks(_data())
    dump = str(blocks)
    assert "780/780" in dump and "100" in dump
    assert "130/150" in dump and "86.6" in dump   # 86.67%
    assert "peer 1" in dump.lower() or "cluster_peer 1" in dump.lower() or "node 1" in dump.lower()
    assert "high-inclusion-delay" in dump

def test_failed_status_shows_error():
    blocks = build_blocks(_data(status="failed"))
    assert "failed" in str(blocks).lower()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_report.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/report.py
from dataclasses import dataclass, field
from dappnode_cycler.combos import Combo
from dappnode_cycler.metrics import WorstNode, HealthCheck
from dappnode_cycler.host_sampler import HostStats

_EMOJI = {"ok": "✅", "degraded": "⚠️", "failed": "❌"}


@dataclass
class ReportData:
    combo: Combo
    cycle: int
    status: str
    cl_image: str
    vc_image: str
    charon_image: str
    window: str
    worst_node: WorstNode | None
    charon_mem_bytes: float | None
    charon_cpu: float | None
    host: HostStats | None
    health: list = field(default_factory=list)
    error: str | None = None


def _gb(x):
    return "n/a" if x is None else f"{x / 1e9:.2f} GB"


def build_text(data: ReportData) -> str:
    e = _EMOJI.get(data.status, "")
    return f"{e} {data.combo.cl} → charon → {data.combo.vc} · cycle {data.cycle} · {data.status}"


def _duties_md(wn: WorstNode) -> str:
    if wn is None or not wn.duties:
        return "_no duty data_"
    lines = [f"*Duties (worst node {wn.peer}):*"]
    for d in wn.duties:
        lines.append(f"• {d.duty}: {int(d.success)}/{int(d.expected)} — {d.pct:.2f}%")
    return "\n".join(lines)


def _health_md(health) -> str:
    if not health:
        return "*Health checks:* none fired ✅"
    lines = ["*Health checks fired:*"]
    for h in health:
        mark = "✖ still firing" if h.firing_now else "✔ cleared"
        lines.append(f"• {h.name} ({h.severity}) — {mark}")
    return "\n".join(lines)


def build_blocks(data: ReportData) -> list:
    e = _EMOJI.get(data.status, "")
    header = f"{e} {data.combo.cl} → charon → {data.combo.vc}"
    blocks = [
        {"type": "header", "text": {"type": "plain_text", "text": header}},
        {"type": "context", "elements": [{"type": "mrkdwn",
            "text": f"cycle {data.cycle} · {data.window} · status *{data.status}*"}]},
        {"type": "section", "fields": [
            {"type": "mrkdwn", "text": f"*CL:* {data.cl_image}"},
            {"type": "mrkdwn", "text": f"*VC:* {data.vc_image}"},
            {"type": "mrkdwn", "text": f"*Charon:* {data.charon_image}"},
        ]},
    ]
    if data.error:
        blocks.append({"type": "section", "text": {"type": "mrkdwn",
            "text": f":x: *Error:* {data.error}"}})
    blocks.append({"type": "section", "text": {"type": "mrkdwn", "text": _duties_md(data.worst_node)}})
    host = data.host
    res = (f"*Charon (worst node):* mem {_gb(data.charon_mem_bytes)}, "
           f"cpu {('n/a' if data.charon_cpu is None else f'{data.charon_cpu:.2f} cores')}\n"
           f"*Host:* cpu {('n/a' if host is None else f'{host.cpu_avg:.0f}% avg / {host.cpu_peak:.0f}% peak')}, "
           f"mem {('n/a' if host is None else f'{_gb(host.mem_avg)} avg / {_gb(host.mem_peak)} peak of {_gb(host.mem_total)}')}")
    blocks.append({"type": "section", "text": {"type": "mrkdwn", "text": res}})
    blocks.append({"type": "section", "text": {"type": "mrkdwn", "text": _health_md(data.health)}})
    return blocks
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_report.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/report.py dappnode-cycler/tests/test_report.py
git commit -m "Add report model and Slack Block Kit builder"
```

---

## Task 10: Slack webhook client

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/slack.py`
- Create: `dappnode-cycler/tests/test_slack.py`

**Interfaces:**
- Produces: `post(webhook_url: str, text: str, blocks: list, http_post=_http_post) -> None`; `_http_post(url: str, data: bytes) -> int` (returns HTTP status). `post` sends `{"text": text, "blocks": blocks}` as JSON and raises `RuntimeError` on non-200.

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_slack.py
import json
import pytest
from dappnode_cycler import slack

def test_post_sends_text_and_blocks():
    captured = {}
    def fake_post(url, data):
        captured["url"] = url
        captured["payload"] = json.loads(data.decode())
        return 200
    slack.post("http://hook", "hello", [{"type": "section"}], http_post=fake_post)
    assert captured["url"] == "http://hook"
    assert captured["payload"] == {"text": "hello", "blocks": [{"type": "section"}]}

def test_post_raises_on_non_200():
    with pytest.raises(RuntimeError):
        slack.post("http://hook", "x", [], http_post=lambda url, data: 500)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_slack.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/slack.py
import json
import urllib.request


def _http_post(url: str, data: bytes) -> int:
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.getcode()


def post(webhook_url: str, text: str, blocks: list, http_post=_http_post) -> None:
    data = json.dumps({"text": text, "blocks": blocks}).encode()
    status = http_post(webhook_url, data)
    if status != 200:
        raise RuntimeError(f"Slack webhook returned HTTP {status}")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_slack.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/slack.py dappnode-cycler/tests/test_slack.py
git commit -m "Add Slack Incoming Webhook client"
```

---

## Task 11: Kurtosis CLI wrappers

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/kurtosis.py`
- Create: `dappnode-cycler/tests/test_kurtosis.py`

**Interfaces:**
- Produces (all take an overridable `runner=subprocess.run`):
  - `run_enclave(enclave: str, package: str, args_file: str, runner=...) -> None`
  - `remove_enclave(enclave: str, runner=...) -> None` (uses `-f`; never raises)
  - `prometheus_base_url(enclave: str, runner=...) -> str | None` (parses `kurtosis port print <enclave> prometheus http`; returns the `http://host:port` URL or `None`)
  - `git_pull(repo_path: str, runner=...) -> None`

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_kurtosis.py
from dappnode_cycler import kurtosis

class FakeCompleted:
    def __init__(self, stdout="", returncode=0):
        self.stdout = stdout
        self.returncode = returncode

def test_run_enclave_builds_command():
    calls = []
    def runner(cmd, **kw):
        calls.append(cmd)
        return FakeCompleted()
    kurtosis.run_enclave("c1-teku-prysm", "github.com/ObolNetwork/ethereum-package@charon",
                         "/tmp/np.yaml", runner=runner)
    cmd = calls[0]
    assert cmd[:2] == ["kurtosis", "run"]
    assert "--enclave" in cmd and "c1-teku-prysm" in cmd
    assert "--args-file" in cmd and "/tmp/np.yaml" in cmd

def test_prometheus_url_parsed():
    def runner(cmd, **kw):
        return FakeCompleted(stdout="http://127.0.0.1:53455\n")
    assert kurtosis.prometheus_base_url("c1-a-b", runner=runner) == "http://127.0.0.1:53455"

def test_prometheus_url_none_on_error():
    def runner(cmd, **kw):
        return FakeCompleted(stdout="", returncode=1)
    assert kurtosis.prometheus_base_url("c1-a-b", runner=runner) is None

def test_remove_enclave_never_raises():
    def runner(cmd, **kw):
        return FakeCompleted(returncode=1)
    kurtosis.remove_enclave("c1-a-b", runner=runner)  # must not raise
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_kurtosis.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/kurtosis.py
import subprocess


def _run(cmd, runner, check=False):
    return runner(cmd, capture_output=True, text=True, check=check)


def run_enclave(enclave, package, args_file, runner=subprocess.run):
    cmd = ["kurtosis", "run", "--enclave", enclave, package, "--args-file", args_file]
    res = _run(cmd, runner)
    if res.returncode != 0:
        raise RuntimeError(f"kurtosis run failed for {enclave}: rc={res.returncode}")


def remove_enclave(enclave, runner=subprocess.run):
    try:
        _run(["kurtosis", "enclave", "rm", "-f", enclave], runner)
    except Exception:
        pass  # teardown is best-effort; never break the cycle


def prometheus_base_url(enclave, runner=subprocess.run):
    res = _run(["kurtosis", "port", "print", enclave, "prometheus", "http"], runner)
    if res.returncode != 0:
        return None
    url = (res.stdout or "").strip().splitlines()[-1].strip() if res.stdout.strip() else ""
    return url or None


def git_pull(repo_path, runner=subprocess.run):
    res = _run(["git", "-C", repo_path, "pull", "--ff-only"], runner)
    if res.returncode != 0:
        raise RuntimeError(f"git pull failed in {repo_path}: rc={res.returncode}")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_kurtosis.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/kurtosis.py dappnode-cycler/tests/test_kurtosis.py
git commit -m "Add Kurtosis CLI + git pull wrappers"
```

---

## Task 12: Config loader

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/config.py`
- Create: `dappnode-cycler/config.example.yaml`
- Create: `dappnode-cycler/tests/test_config.py`

**Interfaces:**
- Produces: `@dataclass Config` with `slack_webhook_url: str`, `repo_path: str`, `state_path: str`, `monitoring_token: str = ""`, `package_ref: str = "github.com/ObolNetwork/ethereum-package@charon"`, `run_minutes: int = 90`, `warmup_minutes: int = 15`, `startup_deadline_minutes: int = 25`, `sample_interval_s: int = 15`. `load_config(path: str) -> Config` (YAML; unknown keys ignored; missing required keys raise `KeyError`).

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_config.py
import pytest
from dappnode_cycler.config import load_config, Config

def test_load_with_defaults(tmp_path):
    p = tmp_path / "c.yaml"
    p.write_text("slack_webhook_url: http://hook\nrepo_path: /srv/kurtosis-charon\nstate_path: /var/lib/cycler/state.json\n")
    c = load_config(str(p))
    assert isinstance(c, Config)
    assert c.run_minutes == 90 and c.warmup_minutes == 15
    assert c.package_ref.endswith("ethereum-package@charon")

def test_overrides_and_unknown_ignored(tmp_path):
    p = tmp_path / "c.yaml"
    p.write_text("slack_webhook_url: h\nrepo_path: r\nstate_path: s\nrun_minutes: 30\nbogus: 1\n")
    c = load_config(str(p))
    assert c.run_minutes == 30

def test_missing_required_raises(tmp_path):
    p = tmp_path / "c.yaml"
    p.write_text("repo_path: r\nstate_path: s\n")
    with pytest.raises(KeyError):
        load_config(str(p))
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_config.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/config.py
from dataclasses import dataclass, fields
import yaml


@dataclass
class Config:
    slack_webhook_url: str
    repo_path: str
    state_path: str
    monitoring_token: str = ""
    package_ref: str = "github.com/ObolNetwork/ethereum-package@charon"
    run_minutes: int = 90
    warmup_minutes: int = 15
    startup_deadline_minutes: int = 25
    sample_interval_s: int = 15


def load_config(path: str) -> Config:
    with open(path) as f:
        raw = yaml.safe_load(f) or {}
    known = {f.name for f in fields(Config)}
    kwargs = {k: v for k, v in raw.items() if k in known}
    return Config(**kwargs)  # missing required fields raise TypeError->wrapped below
```

Note: `Config(**kwargs)` raises `TypeError` for missing required args, not `KeyError`. Make the test expectation match by validating explicitly instead:

```python
    required = ["slack_webhook_url", "repo_path", "state_path"]
    for r in required:
        if r not in kwargs:
            raise KeyError(r)
    return Config(**kwargs)
```

Use the explicit-validation version so `test_missing_required_raises` passes with `KeyError`.

Create `config.example.yaml`:

```yaml
# dappnode-cycler/config.example.yaml
slack_webhook_url: "https://hooks.slack.com/services/XXX/YYY/ZZZ"
repo_path: "/home/dappnode/kurtosis-charon"
state_path: "/var/lib/dappnode-cycler/state.json"
monitoring_token: ""          # PROMETHEUS_REMOTE_WRITE_TOKEN; empty disables remote_write auth
package_ref: "github.com/ObolNetwork/ethereum-package@charon"
run_minutes: 90
warmup_minutes: 15
startup_deadline_minutes: 25
sample_interval_s: 15
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_config.py -v`
Expected: PASS. (If `yaml` is unavailable, `pip install pyyaml` in the venv first.)

- [ ] **Step 5: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/config.py dappnode-cycler/config.example.yaml dappnode-cycler/tests/test_config.py
git commit -m "Add cycler config loader and example config"
```

---

## Task 13: Main loop (run_one + orchestration)

**Files:**
- Create: `dappnode-cycler/dappnode_cycler/cycler.py`
- Create: `dappnode-cycler/tests/test_cycler.py`

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `@dataclass Deps` bundling injectable callables/clients: `git_pull(repo)`, `run_enclave(enclave, package, args_file)`, `remove_enclave(enclave)`, `prometheus_base_url(enclave) -> str | None`, `make_prom_client(base_url) -> PrometheusClient`, `make_sampler() -> Sampler`, `slack_post(text, blocks)`, `sleep(seconds)`, `now() -> float`, `write_args_file(combo, token, dir) -> str`, `wait_healthy(prom: PrometheusClient, cluster_name, deadline_s) -> bool`.
  - `run_one(combo: Combo, cycle: int, cfg: Config, deps: Deps) -> ReportData` — performs a single combo run and returns the report (also posts to Slack).
  - `collect_report(prom, combo, cycle, cfg, window_s, host_stats, status) -> ReportData` — pure-ish assembly from queries.
  - `main(config_path: str) -> None` — the forever loop: load state, teardown any `current_enclave`, select combo, `run_one`, advance+save, repeat.

- [ ] **Step 1: Write the failing test**

```python
# dappnode-cycler/tests/test_cycler.py
from dappnode_cycler.combos import Combo
from dappnode_cycler.config import Config
from dappnode_cycler.metrics import Sample
from dappnode_cycler.host_sampler import Sampler
from dappnode_cycler.cycler import run_one, Deps

class FakeProm:
    def __init__(self, table):
        self.table = table  # dict: substring-in-query -> list[Sample]
    def query(self, q):
        for key, val in self.table.items():
            if key in q:
                return val
        return []

class FakeSampler:
    def start(self): pass
    def stop(self): pass
    def summary(self):
        from dappnode_cycler.host_sampler import HostStats
        return HostStats(20.0, 60.0, 4e9, 5e9, 16e9)

def _cfg():
    return Config(slack_webhook_url="h", repo_path="r", state_path="s",
                  run_minutes=90, warmup_minutes=15, startup_deadline_minutes=25)

def _deps(prom, healthy=True, posted=None):
    return Deps(
        git_pull=lambda repo: None,
        run_enclave=lambda e, p, a: None,
        remove_enclave=lambda e: None,
        prometheus_base_url=lambda e: "http://prom",
        make_prom_client=lambda url: prom,
        make_sampler=FakeSampler,
        slack_post=lambda text, blocks: posted.append((text, blocks)),
        sleep=lambda s: None,
        now=lambda: 0.0,
        write_args_file=lambda combo, token, d: "/tmp/np.yaml",
        wait_healthy=lambda prom, cn, dl: healthy,
    )

def test_run_one_happy_path_posts_ok():
    prom = FakeProm({
        "core_tracker_expect_duties_total": [Sample({"duty": "attester", "cluster_peer": "0"}, 780)],
        "core_tracker_success_duties_total": [Sample({"duty": "attester", "cluster_peer": "0"}, 780)],
        "process_resident_memory_bytes": [Sample({"cluster_peer": "0"}, 5e8)],
        "process_cpu_seconds_total": [Sample({"cluster_peer": "0"}, 1.2)],
        "max_over_time(app_health_checks": [],
        "== 1": [],
    })
    posted = []
    data = run_one(Combo("teku", "prysm"), 1, _cfg(), _deps(prom, healthy=True, posted=posted))
    assert data.status == "ok"
    assert data.worst_node.duties[0].success == 780
    assert len(posted) == 1

def test_run_one_startup_failure_posts_failed():
    posted = []
    data = run_one(Combo("teku", "prysm"), 1, _cfg(),
                   _deps(FakeProm({}), healthy=False, posted=posted))
    assert data.status == "failed"
    assert len(posted) == 1
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd dappnode-cycler && python -m pytest tests/test_cycler.py -v`
Expected: FAIL with `ModuleNotFoundError`.

- [ ] **Step 3: Implement**

```python
# dappnode-cycler/dappnode_cycler/cycler.py
import time
from dataclasses import dataclass
from typing import Callable

from charon_matrix.network_params import CHARON_IMAGE, CL_IMAGES, VC_IMAGES
from dappnode_cycler import kurtosis, promql, slack
from dappnode_cycler.combos import Combo, CYCLE, enclave_name
from dappnode_cycler.config import Config, load_config
from dappnode_cycler.host_sampler import Sampler
from dappnode_cycler.metrics import (
    PrometheusClient, select_worst_node, max_value, parse_health,
)
from dappnode_cycler.report import ReportData, build_text, build_blocks
from dappnode_cycler.selection import select_next_combo
from dappnode_cycler.state import State


@dataclass
class Deps:
    git_pull: Callable
    run_enclave: Callable
    remove_enclave: Callable
    prometheus_base_url: Callable
    make_prom_client: Callable
    make_sampler: Callable
    slack_post: Callable
    sleep: Callable
    now: Callable
    write_args_file: Callable
    wait_healthy: Callable


def _fmt_window(start_s: float, end_s: float) -> str:
    f = "%H:%M"
    return f"{time.strftime(f, time.gmtime(start_s))}-{time.strftime(f, time.gmtime(end_s))} UTC"


def collect_report(prom, combo, cycle, window_s, host_stats, status, window_label):
    cn = combo.cluster_name
    expected = prom.query(promql.duty_expected(cn, window_s))
    success = prom.query(promql.duty_success(cn, window_s))
    worst = select_worst_node(expected, success)
    mem = max_value(prom.query(promql.charon_mem_peak(cn, window_s)))
    cpu = max_value(prom.query(promql.charon_cpu_peak(cn, window_s)))
    health = parse_health(
        prom.query(promql.health_fired(cn, window_s)),
        prom.query(promql.health_firing_now(cn)),
    )
    if status == "ok":
        degraded = any(d.pct < 100.0 for d in (worst.duties if worst else [])) or \
            any(h.firing_now for h in health)
        status = "degraded" if degraded else "ok"
    return ReportData(
        combo=combo, cycle=cycle, status=status,
        cl_image=CL_IMAGES.get(combo.cl, combo.cl),
        vc_image=VC_IMAGES.get(combo.vc, combo.vc),
        charon_image=CHARON_IMAGE, window=window_label,
        worst_node=worst, charon_mem_bytes=mem, charon_cpu=cpu,
        host=host_stats, health=health,
    )


def run_one(combo: Combo, cycle: int, cfg: Config, deps: Deps) -> ReportData:
    enclave = enclave_name(combo, cycle)
    deps.remove_enclave(enclave)  # idempotent: clear any stale enclave
    deps.git_pull(cfg.repo_path)
    args_file = deps.write_args_file(combo, cfg.monitoring_token, "/tmp")

    start = deps.now()
    try:
        deps.run_enclave(enclave, cfg.package_ref, args_file)
    except Exception as e:
        data = _failed_report(combo, cycle, f"launch failed: {e}")
        deps.slack_post(build_text(data), build_blocks(data))
        deps.remove_enclave(enclave)
        return data

    base_url = deps.prometheus_base_url(enclave)
    prom = deps.make_prom_client(base_url) if base_url else None
    healthy = bool(prom) and deps.wait_healthy(
        prom, combo.cluster_name, cfg.startup_deadline_minutes * 60)
    if not healthy:
        data = _failed_report(combo, cycle, "cluster did not become healthy before deadline")
        deps.slack_post(build_text(data), build_blocks(data))
        deps.remove_enclave(enclave)
        return data

    sampler = deps.make_sampler()
    sampler.start()
    deadline = start + cfg.run_minutes * 60
    while deps.now() < deadline:
        deps.sleep(min(cfg.sample_interval_s, max(1, deadline - deps.now())))
    sampler.stop()

    end = deps.now()
    window_s = max(1, int(cfg.run_minutes * 60 - cfg.warmup_minutes * 60))
    data = collect_report(prom, combo, cycle, window_s, sampler.summary(), "ok",
                          _fmt_window(start, end))
    deps.slack_post(build_text(data), build_blocks(data))
    deps.remove_enclave(enclave)
    return data


def _failed_report(combo, cycle, error) -> ReportData:
    return ReportData(
        combo=combo, cycle=cycle, status="failed",
        cl_image=CL_IMAGES.get(combo.cl, combo.cl),
        vc_image=VC_IMAGES.get(combo.vc, combo.vc),
        charon_image=CHARON_IMAGE, window="-",
        worst_node=None, charon_mem_bytes=None, charon_cpu=None,
        host=None, health=[], error=error,
    )


def _default_deps(cfg: Config) -> Deps:
    from dappnode_cycler.params import write_args_file

    def wait_healthy(prom, cluster_name, deadline_s):
        waited = 0
        while waited < deadline_s:
            active = prom.query(
                f'core_scheduler_validators_active{{cluster_name="{cluster_name}"}}')
            if active and any(s.value > 0 for s in active):
                return True
            time.sleep(15)
            waited += 15
        return False

    return Deps(
        git_pull=kurtosis.git_pull,
        run_enclave=kurtosis.run_enclave,
        remove_enclave=kurtosis.remove_enclave,
        prometheus_base_url=kurtosis.prometheus_base_url,
        make_prom_client=lambda url: PrometheusClient(url),
        make_sampler=lambda: Sampler(cfg.sample_interval_s),
        slack_post=lambda text, blocks: slack.post(cfg.slack_webhook_url, text, blocks),
        sleep=time.sleep,
        now=time.time,
        write_args_file=write_args_file,
        wait_healthy=wait_healthy,
    )


def main(config_path: str) -> None:
    cfg = load_config(config_path)
    deps = _default_deps(cfg)
    state = State.load(cfg.state_path)
    if state.current_enclave:
        deps.remove_enclave(state.current_enclave)  # interrupted run
        state.current_enclave = None
        state.save(cfg.state_path)
    while True:
        combo, origin = select_next_combo(state)
        state.current_enclave = enclave_name(combo, state.cycle)
        state.save(cfg.state_path)
        try:
            run_one(combo, state.cycle, cfg, deps)
        except Exception:
            pass  # never break the cycle
        state.current_enclave = None
        if origin == "cycle":
            state.advance()
        state.save(cfg.state_path)


if __name__ == "__main__":
    import sys
    main(sys.argv[1] if len(sys.argv) > 1 else "config.yaml")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd dappnode-cycler && python -m pytest tests/test_cycler.py -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `cd dappnode-cycler && python -m pytest -v && python -m pytest ../charon_matrix/tests -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add dappnode-cycler/dappnode_cycler/cycler.py dappnode-cycler/tests/test_cycler.py
git commit -m "Add cycler main loop with injectable deps and resume"
```

---

## Task 14: systemd unit, README, ops wiring

**Files:**
- Create: `dappnode-cycler/cycler.service`
- Create: `dappnode-cycler/README.md`

**Interfaces:** none (ops/docs). No new code; validated by inspection + a dry import.

- [ ] **Step 1: Write the systemd unit**

```ini
# dappnode-cycler/cycler.service
[Unit]
Description=Charon 36-combo dappnode cycler
After=docker.service network-online.target
Wants=docker.service network-online.target

[Service]
Type=simple
User=dappnode
WorkingDirectory=/home/dappnode/kurtosis-charon/dappnode-cycler
ExecStart=/home/dappnode/kurtosis-charon/dappnode-cycler/.venv/bin/python -m dappnode_cycler.cycler /home/dappnode/kurtosis-charon/dappnode-cycler/config.yaml
Restart=always
RestartSec=30
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Write the README**

Document: purpose; the run cycle; how versions are bumped (edit pins in `charon_matrix/network_params.py`, commit, the cycler `git pull`s before each run); install (`python -m venv .venv`, `pip install pyyaml pytest`, copy `config.example.yaml` → `config.yaml`, set `slack_webhook_url` + `monitoring_token`); systemd install (`sudo cp cycler.service /etc/systemd/system/`, `systemctl enable --now cycler`); how to read logs (`journalctl -u cycler -f`); the state file and resume behaviour; and the (unbuilt) override extension point — note that `read_override()` in `selection.py` is where a future `override.json` reader plugs in.

- [ ] **Step 3: Sanity-check the entrypoint imports**

Run: `cd dappnode-cycler && python -c "import dappnode_cycler.cycler"` (with `..` and `.` importable, e.g. `PYTHONPATH=.:.. python -c ...`)
Expected: no ImportError.

- [ ] **Step 4: Commit**

```bash
git add dappnode-cycler/cycler.service dappnode-cycler/README.md
git commit -m "Add systemd unit and README for dappnode cycler"
```

---

## Self-Review

**Spec coverage:**
- Native Kurtosis path, 4 nodes → Tasks 1, 5 (`charon_node_count=4`, harness unmodified). ✓
- git pull before every run → Task 11 (`git_pull`), Task 13 (`run_one` calls it). ✓
- Local Prometheus, query before teardown → Tasks 7, 11 (`prometheus_base_url`), 13 (query then `remove_enclave`). ✓
- 90-min window, warmup exclusion → Task 13 (`window_s = run - warmup`). ✓
- Worst-node duty ratios → Task 7 (`select_worst_node`). ✓
- Charon CPU/mem max across nodes → Tasks 6/7 (`charon_*_peak` + `max_value`). ✓
- Machine total from /proc → Task 8. ✓
- Health checks from `app_health_checks{name,severity}` → Tasks 6/7/9. ✓
- Slack Incoming Webhook, one message/run → Tasks 9, 10, 13. ✓
- Failure → report and continue → Task 13 (`_failed_report`, loop `try/except`). ✓
- systemd + resume from state file → Tasks 3, 13 (`main` resume), 14. ✓
- Priority-override extension point (not built) → Task 4 (`read_override` stub + `select_next_combo`). ✓
- Version pins single source of truth → Task 1 (`charon_matrix`). ✓

**Placeholder scan:** No TBD/TODO; all steps contain runnable code and explicit commands. The two "note" callouts (Task 8 cpu formula, Task 12 KeyError validation) give exact corrections, not deferrals. ✓

**Type consistency:** `Sample`, `DutyResult(.pct)`, `WorstNode(.peer,.duties)`, `HealthCheck(.name,.severity,.firing_now)`, `HostStats(cpu_avg,cpu_peak,mem_avg,mem_peak,mem_total)`, `ReportData`, `Config`, `State`, `Combo(.cl,.vc,.name,.cluster_name)`, `enclave_name(combo,cycle)`, `build_network_params(cl,vc,charon_node_count)`, `promql.*(cluster_name,window_s)` — names and signatures match across producing and consuming tasks. ✓
