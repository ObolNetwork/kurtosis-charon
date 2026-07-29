import json
import os
from dataclasses import dataclass, asdict
from charon_cycler.combos import CYCLE


@dataclass
class State:
    cycle: int = 0
    next_index: int = 0
    current_enclave: str | None = None

    def advance(self) -> None:
        self.next_index += 1
        if self.next_index >= len(CYCLE):
            self.next_index = 0
            self.cycle += 1

    def save(self, path: str) -> None:
        tmp = path + ".tmp"
        with open(tmp, "w") as f:
            json.dump(asdict(self), f, indent=2)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, path)

    @classmethod
    def load(cls, path: str) -> "State":
        if not os.path.exists(path):
            return cls()
        with open(path) as f:
            return cls(**json.load(f))
