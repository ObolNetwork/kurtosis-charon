# Native Charon 36-Combo AWS Test Matrix — Design

**Date:** 2026-07-29
**Repo (changed):** `ObolNetwork/kurtosis-charon` (branch `kalo/native-36-matrix`, off `kalo/native-kurtosis`)
**Harness (unchanged):** `github.com/ObolNetwork/ethereum-package@charon`

## Goal

Test the current `charon:next` (charon main-branch image) against the latest release of
every consensus client (CL / beacon node) and validator client (VC), run **natively**
through the Obol ethereum-package. Launch the full **6 CL × 6 VC = 36** combination matrix
on AWS, one EC2 instance per combo, all started together, uniform `c6a.4xlarge`.

This extends the existing native runner (`kurtosis_aws_runner_native.py`), which today only
knows the 6 hardcoded "diagonal" combos, to cover the full 36-combo matrix.

## Under test vs. harness

- **`obolnetwork/charon:next`** — charon's main-branch image. This is the thing being tested;
  it deliberately stays a moving tag.
- **`ethereum-package@charon`** — the Kurtosis harness that natively integrates charon as a
  `vc_type`. Used unmodified (local `charon` branch is in sync with `origin/charon`).

## Image pins

All client images pinned to explicit versions (not `:latest`). Resolved 2026-07-29.

| Client | Beacon node (CL) image | Validator (VC) image |
|---|---|---|
| lighthouse | `sigp/lighthouse:v8.2.1` | `sigp/lighthouse:v8.2.1` |
| lodestar | `chainsafe/lodestar:v1.45.0` | `chainsafe/lodestar:v1.45.0` |
| nimbus | `statusim/nimbus-eth2:multiarch-v26.7.0` | `statusim/nimbus-validator-client:multiarch-v26.7.0` |
| teku | `consensys/teku:26.7.1` | `consensys/teku:26.7.1` |
| prysm | `gcr.io/prysmaticlabs/prysm/beacon-chain:v7.1.8` | `gcr.io/prysmaticlabs/prysm/validator:v7.1.8` |
| grandine | `sifrai/grandine:2.0.5` (BN only) | — |
| vouch | — | `attestant/vouch:1.13.1` (VC only) |

- **Charon:** `obolnetwork/charon:next`
- **EL:** `ethereum/client-go:v1.17.4` on all (EL not in scope; kept at the existing pin).

**Matrix:** CLs `{lighthouse, lodestar, nimbus, teku, prysm, grandine}` ×
VCs `{lighthouse, lodestar, nimbus, teku, prysm, vouch}` = **36 combos**, geth EL throughout.

## Approach

**Approach A — generate the args-file per combo in Python** (single source of truth; handles
per-combo quirks in code). Rejected alternatives: (B) one template YAML + `envsubst` — the
nimbus conditional block is awkward to inject; (C) commit 36 static YAMLs — 36 near-identical
files is noise and needs regeneration on every bump.

### Runner changes (`kurtosis-aws-runner/kurtosis_aws_runner_native.py`)

- Add `CL_IMAGES` and `VC_IMAGES` dicts (the pins above) and `CHARON_IMAGE = "obolnetwork/charon:next"`.
- Replace the 6-entry `COMBOS` with the generated 36-combo product (`cl` × `vc`), each entry
  carrying `name` (`<cl>-<vc>`), `bn`, `vc`. Enclave name stays `<bn>-charon-<vc>`.
- Add `build_network_params(cl, vc) -> str` that returns the full args-file YAML.
- Cloud-init: write the generated YAML to a temp file, `envsubst '$PROMETHEUS_REMOTE_WRITE_TOKEN'`,
  then `kurtosis run --enclave <bn>-charon-<vc> github.com/ObolNetwork/ethereum-package@charon
  --args-file /tmp/network_params.yaml`. (Same shape as today, only the args-file is generated.)
- Add a local `--dump-configs [DIR]` flag that writes all 36 generated YAMLs locally (no AWS
  calls) so they can be reviewed before launch.
- Instance sizing is uniform: no auto-doubling (unlike the docker-compose runner). Default
  `--instance-type` may stay `c6a.8xlarge` in the script, but this launch passes `c6a.4xlarge`.

### Generated config per combo (`build_network_params`)

Mirrors the existing `network_params_charon_N_*.yaml` files, generalized:

```yaml
participants:
  # 0) vanilla bootstrap validator — keeps the chain alive; also lets grandine sync
  #    (grandine won't bootstrap a fresh chain solo).
  - el_type: geth
    el_image: ethereum/client-go:v1.17.4
    cl_type: lighthouse
    cl_image: sigp/lighthouse:v8.2.1
    use_separate_vc: true
    vc_type: lighthouse
    vc_image: sigp/lighthouse:v8.2.1
    count: 2
    supernode: true

  # 1) <CL> beacon / charon:next / <VC>  (the combo under test)
  - el_type: geth
    el_image: ethereum/client-go:v1.17.4
    cl_type: <CL>
    cl_image: <CL_IMAGES[CL]>
    supernode: true
    use_separate_vc: true
    vc_type: charon
    vc_image: obolnetwork/charon:next
    charon_node_count: 3
    charon_params:
      charon_vc: <VC>
      charon_vc_image: <VC_IMAGES[VC]>
    # only when VC == nimbus:
    vc_extra_env_vars:
      CHARON_FEATURE_SET_ENABLE: json_requests
    count: 1

network_params:
  network: kurtosis
  network_id: "3151908"
  deposit_contract_address: "0x4242424242424242424242424242424242424242"
  seconds_per_slot: 12
  num_validator_keys_per_node: 128
  preregistered_validator_keys_mnemonic: "giant issue aisle ... very lucky have athlete"
  shard_committee_period: 1
  prefunded_accounts: '{"0xb9e79D19f651a941757b35830232E7EFC77E1c79": {"balance": "100000ETH"}}'
wait_for_finalization: false
global_log_level: info
parallel_keystore_generation: false
mev_type: flashbots
mev_params:
  mev_builder_subsidy: 1
prometheus_params:
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
```

The bootstrap lighthouse is bumped to the pinned `v8.2.1` for consistency; no `preset`
override (harness default = mainnet; finality ~6–8 min, within the 60m lifetime).

## Monitoring (verified end-to-end)

The native path already mirrors the non-native (docker-compose) monitoring; no extra work needed
beyond ensuring every generated config carries `prometheus_params` + `additional_services:
[..., prometheus]` and the runner envsubsts the token.

- Each charon node registers a scrape job `…-charon-N` — `charon_launcher.star:342`.
- Jobs collected into `charon_metrics_jobs`, returned up to `main.star:555`, passed to
  `launch_prometheus(...)` at `main.star:1274` with `prometheus_params`.
- `prometheus_launcher.star` builds `remote_write`: `Url = vm.monitoring.gcp.obol.tech/write`,
  `BearerToken = remote_write_token` (⇒ `Authorization: Bearer <token>`, equivalent to the
  non-native `authorization.credentials`), keep-relabel `job =~ .*charon.*`.
- Per-combo distinguishability: charon `--name=kurtosis-<cl>-<vc>` (`charon_launcher.star:119`,
  `client_name` = CL type) ⇒ distinct `cluster_name` label per combo in Grafana. Matches the
  non-native `CLUSTER_NAME=kurtosis-<cl>-<vc>` convention exactly.

**Non-native reference (for parity):** `setup_monitoring.sh` appends a `remote_write` block with
`url: https://vm.monitoring.gcp.obol.tech/write`, `authorization.credentials: <token>`, and
`write_relabel_configs` keeping `job =~ charon(.*)|otelcollector`; the `CLUSTER_NAME` env sets the
charon cluster name. The native path reproduces every one of these.

## Kurtosis version (required)

The cloud-init installs a **pinned Kurtosis CLI `1.20.0`** from the release-artifacts
`.deb`, not `apt install kurtosis-cli`. The `apt.fury.io/kurtosis-tech` repo is stale
(tops out at `1.15.2`), and `1.15.2` lacks the `GpuConfig` Starlark builtin the
`ethereum-package@charon` harness references (`src/zkboost/zkboost_launcher.star`).
Because Starlark resolves globals at compile time, the whole `main.star` fails to load
with `undefined: GpuConfig`, the enclave is created with **zero services**, and nothing
reaches Grafana. Validated end-to-end on a live instance: `1.20.0` loads the package,
starts 29 services, and remote-writes charon metrics (`cluster_name=kurtosis-<cl>-<vc>`).
Bump `KURTOSIS_VERSION` in the runner if the harness later needs a newer builtin.

## Launch parameters

- One EC2 per combo = **36 instances**, all launched in a single run so they start together.
- Instance type: `c6a.4xlarge`, uniform (no auto-doubling).
- Market: **on-demand** (36 spot instances risk interruptions / capacity errors).
- Lifetime: **60m** (self-terminate via `InstanceInitiatedShutdownBehavior=terminate` + scheduled shutdown).
- Region/subnet/SG: existing hardcoded `eu-west-1c` (`subnet-000b1456766381ae9`, `sg-0e208fd6ad761cafc`), key `kurtosis-fleet`, 50 GB gp3.
- Requires `PROMETHEUS_REMOTE_WRITE_TOKEN` in env and AWS SSO login.
- Runner clones `kurtosis-charon` at `--branch=kalo/native-36-matrix`, so the branch must be
  **pushed** before launch.
- Estimated cost ≈ 36 × ~$0.61/hr ≈ **~$22** for the hour.

## Accepted risks / notes

- **Sizing:** teku/prysm/grandine beacon nodes and teku/vouch VCs are CPU-heavy; on a uniform
  `c6a.4xlarge` (16 vCPU / 32 GB) some heavy combos may run hot or lag. Uniform sizing is an
  explicit choice; revisit if specific combos fail to finalize.
- **MEV:** `flashbots` is kept (matching existing configs) despite the known "mock/flashbots
  serves local-only blocks" issue — MEV correctness is out of scope for this run.
- **Spot vs on-demand:** on-demand chosen for reliability at 36-instance fan-out.

## Out of scope

- Modifying the `ethereum-package@charon` harness.
- Varying the EL (geth only).
- Auto-doubling instance size for heavy combos.
- Any post-run automated assertion/collection of results (manual inspection via Grafana +
  `kurtosis`/`docker logs` on the instances).
