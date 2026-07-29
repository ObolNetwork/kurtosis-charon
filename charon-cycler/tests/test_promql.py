from charon_cycler import promql

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
