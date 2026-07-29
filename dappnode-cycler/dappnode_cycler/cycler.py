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
    total_s = cfg.run_minutes * 60
    elapsed = 0
    while elapsed < total_s:
        step = min(cfg.sample_interval_s, total_s - elapsed)
        deps.sleep(step)
        elapsed += step
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
