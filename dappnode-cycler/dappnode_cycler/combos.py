from dataclasses import dataclass

from charon_matrix.network_params import CLS, VCS


@dataclass(frozen=True)
class Combo:
    cl: str
    vc: str

    @property
    def name(self) -> str:
        return f"{self.cl}-{self.vc}"

    @property
    def cluster_name(self) -> str:
        return f"kurtosis-{self.cl}-{self.vc}"


CYCLE = [Combo(cl, vc) for cl in CLS for vc in VCS]


def enclave_name(combo: Combo, cycle: int) -> str:
    return f"c{cycle}-{combo.cl}-{combo.vc}"
