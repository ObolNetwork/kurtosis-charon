import threading
from dataclasses import dataclass


@dataclass
class HostStats:
    cpu_avg: float
    cpu_peak: float
    mem_avg: float
    mem_peak: float
    mem_total: float


def parse_cpu_line(text: str):
    parts = text.splitlines()[0].split()[1:]
    nums = [float(x) for x in parts]
    total = sum(nums)
    idle = nums[3] + (nums[4] if len(nums) > 4 else 0.0)  # idle + iowait
    return total - idle, total


def cpu_percent(prev, cur):
    busy = cur[0] - prev[0]
    total = cur[1] - prev[1]
    return 0.0 if total <= 0 else 100.0 * busy / total


def parse_meminfo(text: str):
    vals = {}
    for line in text.splitlines():
        k, _, rest = line.partition(":")
        vals[k.strip()] = float(rest.strip().split()[0]) * 1024  # kB -> bytes
    total = vals["MemTotal"]
    avail = vals.get("MemAvailable", 0.0)
    return total - avail, total


class Sampler:
    def __init__(self, interval_s: int = 15):
        self.interval_s = interval_s
        self._cpu_samples = []
        self._mem_samples = []
        self._mem_total = 0.0
        self._prev_cpu = None
        self._stop = threading.Event()
        self._thread = None

    def _read_stat(self) -> str:
        with open("/proc/stat") as f:
            return f.read()

    def _read_meminfo(self) -> str:
        with open("/proc/meminfo") as f:
            return f.read()

    def _sample_once(self):
        cur = parse_cpu_line(self._read_stat())
        if self._prev_cpu is not None:
            self._cpu_samples.append(cpu_percent(self._prev_cpu, cur))
        self._prev_cpu = cur
        used, total = parse_meminfo(self._read_meminfo())
        self._mem_samples.append(used)
        self._mem_total = total

    def _loop(self):
        self._sample_once()  # prime cpu baseline
        while not self._stop.wait(self.interval_s):
            self._sample_once()

    def start(self):
        self._stop.clear()
        self._thread = threading.Thread(target=self._loop, daemon=True)
        self._thread.start()

    def stop(self):
        self._stop.set()
        if self._thread:
            self._thread.join(timeout=5)

    def summary(self) -> HostStats:
        cpu = self._cpu_samples or [0.0]
        mem = self._mem_samples or [0.0]
        return HostStats(
            cpu_avg=sum(cpu) / len(cpu),
            cpu_peak=max(cpu),
            mem_avg=sum(mem) / len(mem),
            mem_peak=max(mem),
            mem_total=self._mem_total,
        )
