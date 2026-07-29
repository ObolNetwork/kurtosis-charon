from charon_cycler.combos import Combo
from charon_cycler.params import build_args_file, write_args_file


def test_four_nodes_and_token_substituted():
    y = build_args_file(Combo("lighthouse", "teku"), token="SECRET123")
    assert "charon_node_count: 4" in y
    assert "$PROMETHEUS_REMOTE_WRITE_TOKEN" not in y
    assert "SECRET123" in y


def test_write_args_file(tmp_path):
    path = write_args_file(Combo("prysm", "vouch"), "tok", str(tmp_path))
    assert path.endswith("network_params.yaml")
    assert "charon_vc: vouch" in open(path).read()
