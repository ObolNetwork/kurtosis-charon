from charon_cycler import kurtosis


class FakeCompleted:
    def __init__(self, stdout="", returncode=0):
        self.stdout = stdout
        self.returncode = returncode


def test_run_enclave_builds_command():
    calls = []

    def runner(cmd, **kw):
        calls.append(cmd)
        return FakeCompleted()

    kurtosis.run_enclave("c1-teku-prysm", "github.com/ObolNetwork/ethereum-package@charon",
                         "/tmp/np.yaml", runner=runner)
    cmd = calls[0]
    assert cmd[:2] == ["kurtosis", "run"]
    assert "--enclave" in cmd and "c1-teku-prysm" in cmd
    assert "--args-file" in cmd and "/tmp/np.yaml" in cmd


def test_prometheus_url_parsed():
    def runner(cmd, **kw):
        return FakeCompleted(stdout="http://127.0.0.1:53455\n")

    assert kurtosis.prometheus_base_url("c1-a-b", runner=runner) == "http://127.0.0.1:53455"


def test_prometheus_url_none_on_error():
    def runner(cmd, **kw):
        return FakeCompleted(stdout="", returncode=1)

    assert kurtosis.prometheus_base_url("c1-a-b", runner=runner) is None


def test_remove_enclave_never_raises():
    def runner(cmd, **kw):
        return FakeCompleted(returncode=1)

    kurtosis.remove_enclave("c1-a-b", runner=runner)  # must not raise


def test_remove_enclave_uses_force_flag():
    calls = []

    def runner(cmd, **kw):
        calls.append(cmd)
        return FakeCompleted()

    kurtosis.remove_enclave("c1-a-b", runner=runner)
    cmd = calls[0]
    assert cmd[:3] == ["kurtosis", "enclave", "rm"]
    assert "-f" in cmd and "c1-a-b" in cmd


def test_run_enclave_raises_on_failure():
    def runner(cmd, **kw):
        return FakeCompleted(returncode=1)

    try:
        kurtosis.run_enclave("c1-a-b", "some/package", "/tmp/np.yaml", runner=runner)
        assert False, "expected RuntimeError"
    except RuntimeError:
        pass


def test_git_pull_builds_command():
    calls = []

    def runner(cmd, **kw):
        calls.append(cmd)
        return FakeCompleted()

    kurtosis.git_pull("/repo/path", runner=runner)
    cmd = calls[0]
    assert cmd == ["git", "-C", "/repo/path", "pull", "--ff-only"]


def test_git_pull_raises_on_failure():
    def runner(cmd, **kw):
        return FakeCompleted(returncode=1)

    try:
        kurtosis.git_pull("/repo/path", runner=runner)
        assert False, "expected RuntimeError"
    except RuntimeError:
        pass
