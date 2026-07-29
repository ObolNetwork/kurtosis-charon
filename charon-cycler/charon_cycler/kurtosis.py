"""Thin wrappers around the Kurtosis CLI and `git pull`.

Every function accepts an overridable `runner` parameter (defaulting to
`subprocess.run`) so callers -- and tests -- can inject a fake instead of
shelling out for real.
"""
import subprocess


def _run(cmd, runner, check=False):
    return runner(cmd, capture_output=True, text=True, check=check)


def run_enclave(enclave, package, args_file, runner=subprocess.run):
    """Launch an enclave via `kurtosis run`. Raises on non-zero exit."""
    cmd = ["kurtosis", "run", "--enclave", enclave, package, "--args-file", args_file]
    res = _run(cmd, runner)
    if res.returncode != 0:
        raise RuntimeError(f"kurtosis run failed for {enclave}: rc={res.returncode}")


def remove_enclave(enclave, runner=subprocess.run):
    """Tear down an enclave via `kurtosis enclave rm -f`. Never raises --
    teardown is best-effort and must never break the cycle."""
    try:
        _run(["kurtosis", "enclave", "rm", "-f", enclave], runner)
    except Exception:
        pass


def prometheus_base_url(enclave, runner=subprocess.run):
    """Resolve the in-enclave Prometheus URL via `kurtosis port print`.

    Returns the `http://host:port` string, or None on error/empty output.
    """
    res = _run(["kurtosis", "port", "print", enclave, "prometheus", "http"], runner)
    if res.returncode != 0:
        return None
    stdout = res.stdout or ""
    url = stdout.strip().splitlines()[-1].strip() if stdout.strip() else ""
    return url or None


def git_pull(repo_path, runner=subprocess.run):
    """Run `git pull --ff-only` in repo_path. Raises on failure."""
    res = _run(["git", "-C", repo_path, "pull", "--ff-only"], runner)
    if res.returncode != 0:
        raise RuntimeError(f"git pull failed in {repo_path}: rc={res.returncode}")
