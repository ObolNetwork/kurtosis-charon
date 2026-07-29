CHARON_IMAGE = "obolnetwork/charon:next"
EL_IMAGE = "ethereum/client-go:v1.17.4"
BOOTSTRAP_CL_IMAGE = "sigp/lighthouse:v8.2.1"

CL_IMAGES = {
    "lighthouse": "sigp/lighthouse:v8.2.1",
    "lodestar": "chainsafe/lodestar:v1.45.0",
    "nimbus": "statusim/nimbus-eth2:multiarch-v26.7.0",
    "teku": "consensys/teku:26.7.1",
    "prysm": "gcr.io/prysmaticlabs/prysm/beacon-chain:v7.1.8",
    "grandine": "sifrai/grandine:2.0.5",
}
VC_IMAGES = {
    "lighthouse": "sigp/lighthouse:v8.2.1",
    "lodestar": "chainsafe/lodestar:v1.45.0",
    "nimbus": "statusim/nimbus-validator-client:multiarch-v26.7.0",
    "teku": "consensys/teku:26.7.1",
    "prysm": "gcr.io/prysmaticlabs/prysm/validator:v7.1.8",
    "vouch": "attestant/vouch:1.13.1",
}
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
    vc_image: {CHARON_IMAGE}
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
