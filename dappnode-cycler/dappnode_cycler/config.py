from dataclasses import dataclass, fields
import yaml


@dataclass
class Config:
    slack_webhook_url: str
    repo_path: str
    state_path: str
    monitoring_token: str = ""
    package_ref: str = "github.com/ObolNetwork/ethereum-package@charon"
    run_minutes: int = 90
    warmup_minutes: int = 15
    startup_deadline_minutes: int = 25
    sample_interval_s: int = 15


def load_config(path: str) -> Config:
    with open(path) as f:
        raw = yaml.safe_load(f) or {}
    known = {f.name for f in fields(Config)}
    kwargs = {k: v for k, v in raw.items() if k in known}

    required = ["slack_webhook_url", "repo_path", "state_path"]
    for r in required:
        if r not in kwargs:
            raise KeyError(r)

    return Config(**kwargs)
