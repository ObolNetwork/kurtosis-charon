from charon_cycler.combos import Combo
from charon_cycler.metrics import WorstNode, DutyResult, HealthCheck
from charon_cycler.host_sampler import HostStats
from charon_cycler.report import ReportData, build_text, build_blocks


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
