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
