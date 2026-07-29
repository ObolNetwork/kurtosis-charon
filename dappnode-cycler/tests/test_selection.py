from dappnode_cycler.combos import Combo
from dappnode_cycler.state import State
from dappnode_cycler.selection import select_next_combo, read_override


def test_default_reader_returns_none():
    assert read_override() is None


def test_selects_from_cycle_when_no_override():
    combo, origin = select_next_combo(State(next_index=6), override_reader=lambda: None)
    assert origin == "cycle"
    assert combo == Combo("lodestar", "lighthouse")


def test_override_takes_priority_without_advancing():
    st = State(next_index=6)
    combo, origin = select_next_combo(st, override_reader=lambda: {"cl": "prysm", "vc": "teku"})
    assert origin == "override"
    assert combo == Combo("prysm", "teku")
    assert st.next_index == 6  # cycle position untouched
