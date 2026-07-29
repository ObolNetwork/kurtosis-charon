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
