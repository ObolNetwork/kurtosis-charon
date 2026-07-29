import os

from charon_matrix.network_params import build_network_params
from dappnode_cycler.combos import Combo


def build_args_file(combo: Combo, token: str, charon_node_count: int = 4) -> str:
    raw = build_network_params(combo.cl, combo.vc, charon_node_count=charon_node_count)
    return raw.replace("$PROMETHEUS_REMOTE_WRITE_TOKEN", token)


def write_args_file(combo: Combo, token: str, dir_path: str, charon_node_count: int = 4) -> str:
    path = os.path.join(dir_path, "network_params.yaml")
    with open(path, "w") as f:
        f.write(build_args_file(combo, token, charon_node_count))
    return path
