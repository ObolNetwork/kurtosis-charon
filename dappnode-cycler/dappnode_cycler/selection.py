from dappnode_cycler.combos import Combo, CYCLE
from dappnode_cycler.state import State


def read_override():
    """Extension point (not built yet): return {"cl","vc"[,"sticky"]} or None.

    A future implementation reads dappnode-cycler/override.json. Until then this
    stub returns None so the cycle runs normally, while select_next_combo already
    handles the override branch.
    """
    return None


def combo_from_override(ov: dict) -> Combo:
    return Combo(ov["cl"], ov["vc"])


def select_next_combo(state: State, override_reader=read_override):
    ov = override_reader()
    if ov:
        return combo_from_override(ov), "override"
    return CYCLE[state.next_index], "cycle"
