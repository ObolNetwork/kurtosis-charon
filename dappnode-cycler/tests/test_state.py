from dappnode_cycler.state import State


def test_defaults_and_roundtrip(tmp_path):
    p = str(tmp_path / "state.json")
    assert State.load(p) == State(0, 0, None)     # missing file -> defaults
    s = State(cycle=2, next_index=35, current_enclave="c2-grandine-vouch")
    s.save(p)
    assert State.load(p) == s


def test_advance_wraps_and_bumps_cycle():
    s = State(cycle=0, next_index=34)
    s.advance()
    assert (s.cycle, s.next_index) == (0, 35)
    s.advance()
    assert (s.cycle, s.next_index) == (1, 0)   # wrapped past 36 combos


def test_save_is_atomic(tmp_path):
    p = str(tmp_path / "state.json")
    State(1, 5, None).save(p)
    import os
    assert not any(f.endswith(".tmp") for f in os.listdir(tmp_path))  # no temp left behind
