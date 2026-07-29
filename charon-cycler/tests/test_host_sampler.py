from charon_cycler.host_sampler import (
    parse_cpu_line, cpu_percent, parse_meminfo, Sampler, HostStats,
)

STAT = "cpu  100 0 100 800 0 0 0 0 0 0\n"


def test_parse_cpu_line():
    busy, total = parse_cpu_line(STAT)
    # total = 100+0+100+800 = 1000, idle = idle(800) + iowait(0) = 800
    # busy = total - idle = 1000 - 800 = 200
    assert (busy, total) == (200, 1000)


def test_cpu_percent_over_interval():
    prev = (300, 1000)
    cur = (450, 1200)   # +150 busy of +200 total -> 75%
    assert cpu_percent(prev, cur) == 75.0


def test_parse_meminfo():
    text = "MemTotal: 1000 kB\nMemAvailable: 250 kB\n"
    used, total = parse_meminfo(text)
    assert total == 1000 * 1024
    assert used == 750 * 1024


def test_sampler_summary_avg_and_peak():
    s = Sampler(interval_s=0)
    stats = [("cpu  100 0 100 800 0 0 0 0 0 0\n", "MemTotal: 1000 kB\nMemAvailable: 500 kB\n"),
             ("cpu  250 0 200 950 0 0 0 0 0 0\n", "MemTotal: 1000 kB\nMemAvailable: 250 kB\n")]
    it = iter(stats)

    def rd():
        s._pending = next(it)
    # feed two samples manually
    s._read_stat = lambda: s._pending[0]
    s._read_meminfo = lambda: s._pending[1]
    rd(); s._sample_once()
    rd(); s._sample_once()
    out = s.summary()
    assert isinstance(out, HostStats)
    assert out.mem_peak == 750 * 1024   # second sample used 750kB
    assert out.mem_total == 1000 * 1024
    assert 0.0 <= out.cpu_avg <= 100.0 and out.cpu_peak >= out.cpu_avg


def test_sampler_restart_clears_stop_event():
    s = Sampler(interval_s=0.01)
    s._read_stat = lambda: "cpu  100 0 100 800 0 0 0 0 0 0\n"
    s._read_meminfo = lambda: "MemTotal: 1000 kB\nMemAvailable: 500 kB\n"

    s.start()
    assert s._thread.is_alive()
    s.stop()
    assert s._stop.is_set()
    assert not s._thread.is_alive()

    # Restarting the same instance must clear the stop event, otherwise
    # _loop's `while not self._stop.wait(...)` exits immediately after the
    # single priming sample (silent one-shot degradation).
    s.start()
    assert not s._stop.is_set()
    assert s._thread.is_alive()
    s.stop()
    assert not s._thread.is_alive()
