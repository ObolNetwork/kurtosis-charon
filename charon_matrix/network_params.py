import json as _json
import os as _os

_IMAGES = _json.load(open(_os.path.join(_os.path.dirname(_os.path.dirname(__file__)), "images.json")))
DV_IMAGE = _IMAGES["dv"]
EL_IMAGE = _IMAGES["el"]
BOOTSTRAP_CL_IMAGE = _IMAGES["bootstrap_cl"]
CL_IMAGES = _IMAGES["cl"]
VC_IMAGES = _IMAGES["vc"]

CLS = ["lighthouse", "lodestar", "nimbus", "teku", "prysm", "grandine"]
VCS = ["lighthouse", "lodestar", "nimbus", "teku", "prysm", "vouch"]
VALIDATOR_KEYS_MNEMONIC = (
    "giant issue aisle success illegal bike spike question tent bar rely arctic "
    "volcano long crawl hungry vocal artwork sniff fantasy very lucky have athlete"
)


def build_network_params(cl, vc, charon_node_count=3):
    """Return the full ethereum-package args-file YAML for one CL x VC combo.

    Bootstrap 2x geth/lighthouse supernode keeps the chain alive; participant 1 is
    the combo under test (<cl> beacon / charon:next / <vc>). The
    $PROMETHEUS_REMOTE_WRITE_TOKEN placeholder is left intact for later substitution.
    """
    nimbus_env = ""
    if vc == "nimbus":
        nimbus_env = (
            "    vc_extra_env_vars:\n"
            "      CHARON_FEATURE_SET_ENABLE: json_requests\n"
        )
    return f"""participants:
  - el_type: geth
    el_image: {EL_IMAGE}
    cl_type: lighthouse
    cl_image: {BOOTSTRAP_CL_IMAGE}
    use_separate_vc: true
    vc_type: lighthouse
    vc_image: {BOOTSTRAP_CL_IMAGE}
    count: 2
    supernode: true

  - el_type: geth
    el_image: {EL_IMAGE}
    cl_type: {cl}
    cl_image: {CL_IMAGES[cl]}
    supernode: true
    use_separate_vc: true
    vc_type: charon
    vc_image: {DV_IMAGE}
    charon_node_count: {charon_node_count}
    charon_params:
      charon_vc: {vc}
      charon_vc_image: {VC_IMAGES[vc]}
{nimbus_env}    count: 1
network_params:
  network: kurtosis
  network_id: "3151908"
  deposit_contract_address: "0x4242424242424242424242424242424242424242"
  seconds_per_slot: 12
  num_validator_keys_per_node: 128
  preregistered_validator_keys_mnemonic: "{VALIDATOR_KEYS_MNEMONIC}"
  shard_committee_period: 1
  prefunded_accounts: '{{"0xb9e79D19f651a941757b35830232E7EFC77E1c79": {{"balance": "100000ETH"}}}}'
wait_for_finalization: false
global_log_level: info
parallel_keystore_generation: false
mev_type: flashbots
mev_params:
  mev_builder_subsidy: 1
prometheus_params:
  storage_tsdb_retention_time: 3h
  remote_write_url: "https://vm.monitoring.gcp.obol.tech/write"
  remote_write_token: "$PROMETHEUS_REMOTE_WRITE_TOKEN"
  remote_write_relabel_configs:
    - SourceLabels: ["job"]
      Regex: ".*charon.*"
      Action: keep
    - SourceLabels: ["client_name"]
      Regex: "charon"
      TargetLabel: job
      Replacement: charon
      Action: replace
additional_services:
  - spamoor
  - prometheus
"""
