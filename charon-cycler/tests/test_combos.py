from charon_cycler.combos import Combo, CYCLE, enclave_name


def test_cycle_is_36_cl_major():
    assert len(CYCLE) == 36
    assert CYCLE[0] == Combo("lighthouse", "lighthouse")
    assert CYCLE[6] == Combo("lodestar", "lighthouse")  # CL-major: 7th entry rolls CL
    assert CYCLE[-1] == Combo("grandine", "vouch")


def test_names():
    c = Combo("teku", "prysm")
    assert c.name == "teku-prysm"
    assert c.cluster_name == "kurtosis-teku-prysm"
    assert enclave_name(c, 3) == "c3-teku-prysm"
