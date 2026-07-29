import pytest
from charon_cycler.config import load_config, Config

def test_load_with_defaults(tmp_path):
    p = tmp_path / "c.yaml"
    p.write_text("slack_webhook_url: http://hook\nrepo_path: /srv/kurtosis-charon\nstate_path: /var/lib/cycler/state.json\n")
    c = load_config(str(p))
    assert isinstance(c, Config)
    assert c.run_minutes == 90 and c.warmup_minutes == 15
    assert c.package_ref.endswith("ethereum-package@charon")

def test_overrides_and_unknown_ignored(tmp_path):
    p = tmp_path / "c.yaml"
    p.write_text("slack_webhook_url: h\nrepo_path: r\nstate_path: s\nrun_minutes: 30\nbogus: 1\n")
    c = load_config(str(p))
    assert c.run_minutes == 30

def test_missing_required_raises(tmp_path):
    p = tmp_path / "c.yaml"
    p.write_text("repo_path: r\nstate_path: s\n")
    with pytest.raises(KeyError):
        load_config(str(p))
