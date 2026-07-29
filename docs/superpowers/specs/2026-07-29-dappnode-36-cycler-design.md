# Dappnode 24/7 Sequential 36-Combo Cycler — Design

**Date:** 2026-07-29
**Repo (new code):** `ObolNetwork/kurtosis-charon`, new dir `dappnode-cycler/`
**Harness (unchanged):** `github.com/ObolNetwork/ethereum-package@charon` (native Charon integration)

## Goal

Continuously exercise `charon:next` against the full **6 CL × 6 VC = 36** matrix on the
dappnode, one combo at a time, 24/7. Each combo runs for **90 minutes**, is then torn down,
and its results are posted to **Slack**. The cycle repeats forever, picking up the latest
client/Charon versions from git between runs.

This is the on-prem, sequential counterpart to the AWS parallel runner
(`kurtosis-aws-runner/kurtosis_aws_runner_native.py`). It reuses that runner's
`build_network_params()` as the single source of truth for the per-combo args-file, and
runs the **native** Kurtosis path (`vc_type: charon`) — not the legacy docker-compose
`make geth-<cl>-charon-<vc>` path.

## Under test vs. harness

- **`obolnetwork/charon:next`** — the thing under test; deliberately a moving tag.
- **`ethereum-package@charon`** — the native harness, used unmodified. Kurtosis fetches it
  from the remote `@charon` branch each run, so harness updates arrive automatically.
- **4 Charon nodes per cluster** — the harness field `charon_node_count` is fully
  parametrized (drives `--nodes=N`, per-node services, and per-node Prometheus scrape jobs
  in `src/vc/charon_launcher.star`) and already defaults to `4`
  (`src/package_io/input_parser.star:2113`). All Charon nodes point at the same beacon node
  (`charon_launcher.star:86`), so a 4th node needs no extra BN wiring. The cycler sets
  `charon_node_count: 4` explicitly.

## Decisions (from brainstorming)

| Topic | Decision |
|---|---|
| Language | **Python** (matches existing tooling; concise HTTP/Prometheus/Slack glue) |
| Metrics source | **In-enclave Prometheus** (service `prometheus`, HTTP port; PromQL identical to the Grafana panels). Queried **before** teardown. |
| Version updates | **`git pull` before every single run**; version pins live in the git-tracked runner module. |
| Slack delivery | **Incoming Webhook**, one Block Kit message per run. |
| Failure handling | **Report failure, continue** — the 24/7 cycle never halts. |
| Window / warmup | **90 min wall-clock**; duty ratios computed over `[genesis + warmup, end]`, warmup default ~15 min (~2 epochs), configurable. |
| Cluster size | **4 Charon nodes**. |
| Duty reporting | **Worst node** — the `cluster_peer` with the fewest successful duties (worst case, not a sum). |
| Charon CPU/mem | **Max across the 4 nodes** (worst case), chosen independently per metric. |
| Machine total | **Whole dappnode host** (only one combo runs at a time), sampled from `/proc` by the runner. |
| Process host | **systemd service**, auto-start + restart-on-crash, resume from a state file after reboot. |

## Components (all new, under `kurtosis-charon/dappnode-cycler/`)

1. **`cycler.py`** — the main loop. Owns state, combo selection, git pull, launch, wait,
   teardown, and orchestration of the report.
2. **`params.py`** — imports `build_network_params` from
   `kurtosis-aws-runner/kurtosis_aws_runner_native.py`; overrides `charon_node_count` to 4;
   strips AWS/cloud-init concerns (the dappnode runs Kurtosis locally). If importing across
   that path proves awkward, factor the shared generator into a small module both import.
3. **`metrics.py`** — resolves the enclave's Prometheus URL
   (`kurtosis port print <enclave> prometheus http`) and runs the PromQL queries below
   against `/api/v1/query` and `/api/v1/query_range`.
4. **`host_sampler.py`** — samples host CPU%/mem from `/proc/stat` + `/proc/meminfo` on an
   interval for the duration of the window; reports avg + peak.
5. **`slack.py`** — builds the Block Kit payload and POSTs to the Incoming Webhook.
6. **`state.py` + `state.json`** — persisted cycle state for reboot resume.
7. **`cycler.service`** — systemd unit.
8. **`config.yaml`** (or env) — webhook URL, warmup minutes, run minutes, startup deadline,
   Prometheus retention, `PROMETHEUS_REMOTE_WRITE_TOKEN`.

## Per-run flow (90 min wall-clock)

```
for each iteration:
  combo = select_next_combo()          # override-aware (see Extension points)
  git -C <repo> pull --ff-only          # picks up new version pins / cycler changes
  enclave = "c{cycle}-{cl}-{vc}"
  write args-file (build_network_params(cl, vc), charon_node_count=4, token via envsubst)
  kurtosis run --enclave <enclave> github.com/ObolNetwork/ethereum-package@charon --args-file ...
  wait until healthy OR startup-deadline:
      healthy = beacon finalizing AND Charon metrics present for all 4 nodes
      if deadline exceeded -> mark run FAILED (startup)
  record genesis time; sleep until (launch + 90 min), sampling host resources throughout
  query Prometheus (BEFORE teardown) over [genesis+warmup, now]
  post Slack report (success/degraded/failed)
  kurtosis enclave rm -f <enclave>
  advance + persist state
```

Idempotent teardown at the top of the loop as well (remove any leftover enclave from a crash
before launching the next), so reboots never leave a stuck enclave blocking the cycle.

## Metrics & PromQL

All queries filtered by `cluster_name="kurtosis-<cl>-<vc>"`. `RANGE` = the measurement window
`[genesis+warmup, end]`.

**Duty success/fail — worst node.** For each duty in
`{attester, aggregator, proposer, sync_message, sync_contribution, ...}`:
- expected: `sum by (cluster_peer) (increase(core_tracker_expect_duties_total{...,duty="<d>"}[RANGE]))`
- success:  `sum by (cluster_peer) (increase(core_tracker_success_duties_total{...,duty="<d>"}[RANGE]))`

Pick the worst node = the `cluster_peer` minimizing total success (ties → lowest ratio).
Report `success/expected — pct%` per duty for that node. Example:
`Attestations 780/780 — 100%`, `Aggregations 130/150 — 86.67%`.

**Charon CPU/mem — max across 4 nodes (worst case, per-metric):**
- mem: `max(process_resident_memory_bytes{...})` (peak over window via `max_over_time`)
- cpu: `max(rate(process_cpu_seconds_total{...}[5m]))` → peak per-node CPU-core-seconds/s

**Machine total (host):** sampled by `host_sampler.py` (not Prometheus) — avg + peak CPU%,
avg + peak used memory. Rationale: the runner lives on the host and can read `/proc`
directly; only one combo runs at a time, so host totals reflect this stack.

**Health Checks — everything from the metric:** `app_health_checks{cluster_name=...}` by
`(name, severity)`. Report every check that was firing (`== 1`) at any point in the window,
with name, severity, and whether still firing at end. Exceptions/filtering are deferred
(the operator will whitelist known-benign checks later) — the report lists all of them for now.

## Slack report (one message per run)

- **Header:** `✅|⚠️|❌ <cl> → charon → <vc>` · cycle #N · window `HH:MM–HH:MM UTC`.
- **Versions:** CL image, VC image, `charon:next` resolved digest/tag.
- **Duties (worst node <peer>):** one line per duty type, `success/expected — pct%`.
- **Resources:** Charon mem (max node), Charon CPU (max node); host mem avg/peak, host CPU avg/peak.
- **Health checks:** bullet list of `name (severity)` that fired, ✔/✖ still-firing.
- On startup failure: `❌ failed to start` with the last N lines of relevant service logs.

## State & resume

`state.json`: `{ "cycle": <int>, "next_index": <0..35>, "current_enclave": <str|null>,
"override": <see below> }`. Written atomically after every transition. On (re)start the
systemd unit reads it: if `current_enclave` is set, tear it down (it was interrupted), then
continue from `next_index`. Combo ordering is a fixed list (CL-major) so `next_index` is stable.

## Extension points (designed-in, not built now)

**Priority override — force a specific combo ahead of the cycle.** `select_next_combo()` is
the single chokepoint for combo selection:

```
def select_next_combo(state):
    ov = read_override()        # e.g. dappnode-cycler/override.json or a state field
    if ov:                       # {"cl": "...", "vc": "...", "sticky": bool}
        return combo_for(ov), origin="override"
    return CYCLE[state.next_index], origin="cycle"
```

A forced combo takes priority over the regular cycle for the next run. Whether it is
one-shot (cleared after running) or sticky (pinned until removed) is controlled by the
override payload. The cycle position (`next_index`) is **not** advanced by an override run,
so the normal rotation resumes exactly where it left off once the override is cleared. Only
`read_override()` + the override file/schema need to be implemented later; the loop and
state already accommodate it.

## Accepted risks / notes

- **Full-cycle latency:** 36 × ~90 min ≈ **54 h** per complete pass (plus startup/teardown).
  Acceptable — this is a soak, not a fast gate.
- **Prometheus retention:** must exceed the 90-min window; set
  `storage_tsdb_retention_time` comfortably (e.g. 3h) in `prometheus_params`.
- **Query-before-teardown is mandatory:** the in-enclave Prometheus dies with the enclave.
- **Harness cache:** Kurtosis caches remote packages by ref; if a forced-fresh `@charon`
  pull is ever needed, clear the package cache between runs (document, don't automate yet).
- **MEV:** `flashbots` kept (matching existing configs) despite the known mock/local-only-block
  issue; MEV correctness is out of scope.
- **Known per-client issues** (grandine aggregator 404s, nimbus discv5 fork-config) will
  surface in reports; these are client-side and expected, candidates for the future
  health-check/duty exception list.

## Out of scope

- Modifying the `ethereum-package@charon` harness.
- The legacy docker-compose `make` path.
- Varying the EL (geth only).
- Implementing the priority-override reader (extension point only).
- Grafana/alerting integration beyond the Slack message.
