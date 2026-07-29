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
        if payload.get("status") != "success":
            error_type = payload.get("errorType")
            error = payload.get("error")
            raise RuntimeError(
                f"Prometheus query failed: errorType={error_type!r} error={error!r}"
            )
        result = payload.get("data", {}).get("result", [])
        return [Sample(item["metric"], float(item["value"][1])) for item in result]
