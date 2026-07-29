from dappnode_cycler.combos import Combo
from dappnode_cycler.config import Config
from dappnode_cycler.metrics import Sample
from dappnode_cycler.host_sampler import Sampler
from dappnode_cycler.cycler import run_one, Deps, collect_report, compute_backoff, _fmt_window

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


class RaisingProm:
    """Simulates metrics.PrometheusClient.query raising on a non-success status."""
    def query(self, q):
        raise RuntimeError("prometheus query failed")


class TrackingSampler:
    def __init__(self):
        self.stopped = False
    def start(self): pass
    def stop(self):
        self.stopped = True
    def summary(self):
        from dappnode_cycler.host_sampler import HostStats
        return HostStats(20.0, 60.0, 4e9, 5e9, 16e9)


def test_run_one_mid_flow_exception_still_tears_down_and_posts():
    posted = []
    removed = []
    sampler_holder = {}

    def make_sampler():
        s = TrackingSampler()
        sampler_holder["sampler"] = s
        return s

    deps = Deps(
        git_pull=lambda repo: None,
        run_enclave=lambda e, p, a: None,
        remove_enclave=lambda e: removed.append(e),
        prometheus_base_url=lambda e: "http://prom",
        make_prom_client=lambda url: RaisingProm(),
        make_sampler=make_sampler,
        slack_post=lambda text, blocks: posted.append((text, blocks)),
        sleep=lambda s: None,
        now=lambda: 0.0,
        write_args_file=lambda combo, token, d: "/tmp/np.yaml",
        wait_healthy=lambda prom, cn, dl: True,
    )

    data = run_one(Combo("teku", "prysm"), 1, _cfg(), deps)

    assert data.status == "failed"
    assert len(posted) == 1
    assert len(removed) >= 1
    assert sampler_holder["sampler"].stopped is True


def test_run_one_pre_launch_git_pull_failure_posts_failed():
    posted = []

    def raising_git_pull(repo):
        raise RuntimeError("boom")

    deps = Deps(
        git_pull=raising_git_pull,
        run_enclave=lambda e, p, a: None,
        remove_enclave=lambda e: None,
        prometheus_base_url=lambda e: "http://prom",
        make_prom_client=lambda url: FakeProm({}),
        make_sampler=FakeSampler,
        slack_post=lambda text, blocks: posted.append((text, blocks)),
        sleep=lambda s: None,
        now=lambda: 0.0,
        write_args_file=lambda combo, token, d: "/tmp/np.yaml",
        wait_healthy=lambda prom, cn, dl: True,
    )

    data = run_one(Combo("teku", "prysm"), 1, _cfg(), deps)

    assert data.status == "failed"
    assert len(posted) == 1


def test_compute_backoff_escalates_and_caps():
    assert compute_backoff(0, 30, 900) == 30
    assert compute_backoff(1, 30, 900) == 60
    assert compute_backoff(2, 30, 900) == 120
    assert compute_backoff(20, 30, 900) == 900  # capped for a long failure streak


def _worst_node_prom(pct_duty_success, pct_duty_expected=1000):
    return FakeProm({
        "core_tracker_expect_duties_total": [Sample({"duty": "attester", "cluster_peer": "0"}, pct_duty_expected)],
        "core_tracker_success_duties_total": [Sample({"duty": "attester", "cluster_peer": "0"}, pct_duty_success)],
        "process_resident_memory_bytes": [Sample({"cluster_peer": "0"}, 5e8)],
        "process_cpu_seconds_total": [Sample({"cluster_peer": "0"}, 1.2)],
        "max_over_time(app_health_checks": [],
        "== 1": [],
    })


def test_collect_report_stays_ok_within_degraded_tolerance():
    # 999/1000 = 99.9% -- above DEGRADED_PCT_THRESHOLD (99.5), should stay "ok".
    prom = _worst_node_prom(pct_duty_success=999, pct_duty_expected=1000)
    data = collect_report(prom, Combo("teku", "prysm"), 1, 4500, None, "ok", "w")
    assert data.status == "ok"


def test_collect_report_flags_degraded_below_tolerance():
    # 950/1000 = 95% -- below DEGRADED_PCT_THRESHOLD, should flag "degraded".
    prom = _worst_node_prom(pct_duty_success=950, pct_duty_expected=1000)
    data = collect_report(prom, Combo("teku", "prysm"), 1, 4500, None, "ok", "w")
    assert data.status == "degraded"


def test_run_one_window_label_reflects_scored_window_not_startup():
    prom = FakeProm({
        "core_tracker_expect_duties_total": [Sample({"duty": "attester", "cluster_peer": "0"}, 780)],
        "core_tracker_success_duties_total": [Sample({"duty": "attester", "cluster_peer": "0"}, 780)],
        "process_resident_memory_bytes": [Sample({"cluster_peer": "0"}, 5e8)],
        "process_cpu_seconds_total": [Sample({"cluster_peer": "0"}, 1.2)],
        "max_over_time(app_health_checks": [],
        "== 1": [],
    })
    posted = []
    end_time = 1_000_000.0
    deps = _deps(prom, healthy=True, posted=posted)
    deps.now = lambda: end_time  # constant: acts as "end" regardless of call count

    cfg = _cfg()
    data = run_one(Combo("teku", "prysm"), 1, cfg, deps)

    window_s = cfg.run_minutes * 60 - cfg.warmup_minutes * 60
    expected_label = _fmt_window(end_time - window_s, end_time)
    assert data.window == expected_label
