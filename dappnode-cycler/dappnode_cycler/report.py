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
