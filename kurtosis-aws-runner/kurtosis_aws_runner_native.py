import os
import re
import sys
import time
import argparse
from tabulate import tabulate

KEY_NAME = "kurtosis-fleet"
SECURITY_GROUP_ID = "sg-0e208fd6ad761cafc"
SUBNET_ID = "subnet-000b1456766381ae9"  # eu-west-1c
DEFAULT_INSTANCE_TYPE = "c6a.4xlarge"
VOLUME_SIZE = 50
VOLUME_TYPE = "gp3"
VOLUME_IOPS = 6000  # optimized for Charon test runs
VOLUME_THROUGHPUT = 250  # MB/s
BASE_TAG = "kurtosis-fleet"
GIT_REPO = "https://github.com/ObolNetwork/kurtosis-charon.git"
ETHEREUM_PACKAGE = "github.com/ObolNetwork/ethereum-package@charon"
# apt.fury.io/kurtosis-tech is stale at 1.15.2, which lacks the GpuConfig Starlark
# builtin the ethereum-package@charon harness requires; pin a known-good release.
KURTOSIS_VERSION = "1.20.0"

# --- Image pins (resolved 2026-07-29) ---------------------------------------
# Charon under test is the main-branch image; it stays a moving tag on purpose.
CHARON_IMAGE = "obolnetwork/charon:next"
EL_IMAGE = "ethereum/client-go:v1.17.4"
# Vanilla bootstrap node's CL/VC (kept pinned + consistent with CL_IMAGES).
BOOTSTRAP_CL_IMAGE = "sigp/lighthouse:v8.2.1"

# Beacon-node (consensus client) images, keyed by client type.
CL_IMAGES = {
    "lighthouse": "sigp/lighthouse:v8.2.1",
    "lodestar": "chainsafe/lodestar:v1.45.0",
    "nimbus": "statusim/nimbus-eth2:multiarch-v26.7.0",
    "teku": "consensys/teku:26.7.1",
    "prysm": "gcr.io/prysmaticlabs/prysm/beacon-chain:v7.1.8",
    "grandine": "sifrai/grandine:2.0.5",
}

# Validator-client images (the VC Charon drives), keyed by client type.
VC_IMAGES = {
    "lighthouse": "sigp/lighthouse:v8.2.1",
    "lodestar": "chainsafe/lodestar:v1.45.0",
    "nimbus": "statusim/nimbus-validator-client:multiarch-v26.7.0",
    "teku": "consensys/teku:26.7.1",
    "prysm": "gcr.io/prysmaticlabs/prysm/validator:v7.1.8",
    "vouch": "attestant/vouch:1.13.1",
}

# The full 6 CL x 6 VC = 36 matrix. One EC2 instance per combo. "bn"/"vc" name the
# beacon-node and validator-client clients; the enclave is <bn>-charon-<vc>.
CLS = ["lighthouse", "lodestar", "nimbus", "teku", "prysm", "grandine"]
VCS = ["lighthouse", "lodestar", "nimbus", "teku", "prysm", "vouch"]
COMBOS = [{"name": f"{cl}-{vc}", "bn": cl, "vc": vc} for cl in CLS for vc in VCS]

VALIDATOR_KEYS_MNEMONIC = (
    "giant issue aisle success illegal bike spike question tent bar rely arctic "
    "volcano long crawl hungry vocal artwork sniff fantasy very lucky have athlete"
)

# boto3 clients are created lazily in main() so --dump-configs works without AWS.
ec2 = None
ec2_resource = None


def enclave_name(combo):
    return f"{combo['bn']}-charon-{combo['vc']}"


def safe_exit(message):
    print(f"❌ {message}")
    sys.exit(1)


def build_network_params(cl, vc):
    """Return the full ethereum-package args-file YAML for one CL x VC combo.

    Mirrors the deployments/network_params/network_params_charon_*.yaml files,
    generalised to any CL beacon node x any Charon-driven VC:
      - a 2x vanilla geth/lighthouse supernode that bootstraps and keeps the chain
        alive (grandine will not bootstrap a fresh chain solo), and
      - the combo under test: <cl> beacon / charon:next / <vc>.

    The $PROMETHEUS_REMOTE_WRITE_TOKEN placeholder is left intact for envsubst on
    the instance (so dumped configs never contain the secret). Monitoring parity
    with the non-native path: prometheus remote_write to Obol central + charon
    self-labels cluster_name=kurtosis-<cl>-<vc>.
    """
    # Nimbus VC needs Charon's json_requests feature; only emitted for vc=nimbus.
    nimbus_env = ""
    if vc == "nimbus":
        nimbus_env = (
            "    vc_extra_env_vars:\n"
            "      CHARON_FEATURE_SET_ENABLE: json_requests\n"
        )
    return f"""participants:
  # 0) vanilla validator (always kept — bootstraps/keeps the chain alive;
  #    also lets the CL under test sync, since e.g. grandine won't bootstrap solo)
  - el_type: geth
    el_image: {EL_IMAGE}
    cl_type: lighthouse
    cl_image: {BOOTSTRAP_CL_IMAGE}
    use_separate_vc: true
    vc_type: lighthouse
    vc_image: {BOOTSTRAP_CL_IMAGE}
    count: 2
    supernode: true

  # 1) {cl} beacon / charon:next / {vc}  (combo under test)
  - el_type: geth
    el_image: {EL_IMAGE}
    cl_type: {cl}
    cl_image: {CL_IMAGES[cl]}
    supernode: true
    use_separate_vc: true
    vc_type: charon
    vc_image: {CHARON_IMAGE}
    charon_node_count: 3
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


def parse_lifetime_arg(value: str) -> int:
    try:
        if value.isdigit():
            minutes = int(value)
        else:
            match = re.match(r"^(\d+)([mh])$", value.lower())
            if not match:
                raise ValueError()
            num, unit = match.groups()
            minutes = int(num) * (60 if unit == 'h' else 1)
        if not (60 <= minutes <= 480):
            raise ValueError()
        return minutes
    except ValueError:
        safe_exit("Invalid --lifetime. Use formats like '120', '90m', '2h' (min: 60m, max: 480m).")


def get_latest_ubuntu_ami():
    try:
        images = ec2.describe_images(
            Owners=["099720109477"],
            Filters=[
                {"Name": "name", "Values": ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]},
                {"Name": "state", "Values": ["available"]}
            ]
        )["Images"]
        return max(images, key=lambda img: img["CreationDate"])["ImageId"]
    except Exception as e:
        safe_exit(f"Failed to fetch latest Ubuntu AMI: {e}")


def generate_user_data(combo, branch, shutdown_minutes, monitoring_token):
    """Cloud-init that installs Docker + Kurtosis, clones kurtosis-charon, writes this
    combo's generated args-file, substitutes the monitoring token with envsubst, then
    runs the native-Charon ethereum-package (mirrors `make run-native`).
    """
    enclave = enclave_name(combo)
    network_params = build_network_params(combo["bn"], combo["vc"])
    return f"""#!/bin/bash
set -euxo pipefail
sleep 20

# Schedule shutdown early so the instance always self-terminates.
nohup bash -c "sleep {shutdown_minutes}m && /sbin/shutdown -h now" >/home/ubuntu/shutdown.log 2>&1 &

apt-get update -y
apt-get install -y apt-transport-https ca-certificates curl software-properties-common git make jq gettext bash
curl -fsSL https://get.docker.com | sh
usermod -aG docker ubuntu
# Install a pinned Kurtosis CLI from the release artifacts (apt.fury.io is stale
# at 1.15.2, which fails to load the harness with "undefined: GpuConfig").
curl -fsSL -o /tmp/kurtosis-cli.deb "https://github.com/kurtosis-tech/kurtosis-cli-release-artifacts/releases/download/{KURTOSIS_VERSION}/kurtosis-cli_{KURTOSIS_VERSION}_linux_amd64.deb"
apt-get install -y /tmp/kurtosis-cli.deb

su - ubuntu <<'OUTER_EOF'
cd /home/ubuntu
git clone -b {branch} {GIT_REPO}
cd kurtosis-charon
export PROMETHEUS_REMOTE_WRITE_TOKEN="{monitoring_token}"
# Generated args-file for this combo (token placeholder kept for envsubst).
cat > /tmp/network_params_raw.yaml <<'PARAMS_EOF'
{network_params}PARAMS_EOF
envsubst '$PROMETHEUS_REMOTE_WRITE_TOKEN' < /tmp/network_params_raw.yaml > /tmp/network_params.yaml
kurtosis run --enclave {enclave} {ETHEREUM_PACKAGE} --args-file /tmp/network_params.yaml || true
OUTER_EOF
"""


def instance_tag(combo):
    return f"{BASE_TAG}-{combo['name']}"


def instance_exists(tag_value):
    try:
        resp = ec2.describe_instances(
            Filters=[
                {"Name": "tag:Name", "Values": [tag_value]},
                {"Name": "instance-state-name", "Values": ["pending", "running", "stopping", "stopped"]}
            ]
        )
        return any(res["Instances"] for res in resp["Reservations"])
    except Exception as e:
        safe_exit(f"Error checking existing instances: {e}")


def launch_instance(combo, ami_id, branch, shutdown_minutes, monitoring_token, instance_type, on_demand):
    tag = instance_tag(combo)
    if instance_exists(tag):
        print(f"⚠️  Skipping existing instance: {tag}")
        return None, None

    params = {
        "ImageId": ami_id,
        "InstanceType": instance_type,
        "KeyName": KEY_NAME,
        "MinCount": 1,
        "MaxCount": 1,
        "SubnetId": SUBNET_ID,
        "SecurityGroupIds": [SECURITY_GROUP_ID],
        "UserData": generate_user_data(combo, branch, shutdown_minutes, monitoring_token),
        "BlockDeviceMappings": [{
            "DeviceName": "/dev/sda1",
            "Ebs": {
                "VolumeSize": VOLUME_SIZE,
                "VolumeType": VOLUME_TYPE,
                "Iops": VOLUME_IOPS,
                "Throughput": VOLUME_THROUGHPUT,
                "DeleteOnTermination": True
            }
        }],
        "TagSpecifications": [{"ResourceType": "instance", "Tags": [{"Key": "Name", "Value": tag}]}],
        "InstanceInitiatedShutdownBehavior": "terminate"
    }
    if not on_demand:
        params["InstanceMarketOptions"] = {"MarketType": "spot"}

    try:
        resp = ec2.run_instances(**params)
        instance = resp["Instances"][0]
        return instance["InstanceId"], tag
    except Exception as e:
        print(f"❌ Failed to launch {combo['name']}: {e}")
        return None, None


def wait_until_running(instance_ids, tag_map):
    for instance_id in instance_ids:
        name = tag_map.get(instance_id, instance_id)
        print(f"⏳ Waiting for instance {instance_id} ({name}) to be running...")
        try:
            ec2_resource.Instance(instance_id).wait_until_running()
        except Exception as e:
            print(f"⚠️  Failed to wait for {instance_id}: {e}")


def fetch_instance_table(tag_values):
    try:
        resp = ec2.describe_instances(
            Filters=[
                {"Name": "tag:Name", "Values": tag_values},
                {"Name": "instance-state-name", "Values": ["pending", "running", "stopping", "stopped"]}
            ]
        )
        rows = []
        for res in resp["Reservations"]:
            for inst in res["Instances"]:
                name = next((t["Value"] for t in inst["Tags"] if t["Key"] == "Name"), "")
                ip = inst.get("PublicIpAddress", "pending")
                state = inst["State"]["Name"]
                rows.append([name, ip, state])
        return rows
    except Exception as e:
        safe_exit(f"Error fetching instance status: {e}")


def terminate_instances(tag_values):
    try:
        resp = ec2.describe_instances(
            Filters=[
                {"Name": "tag:Name", "Values": tag_values},
                {"Name": "instance-state-name", "Values": ["pending", "running", "stopping", "stopped"]}
            ]
        )
        instance_map = {}
        for res in resp["Reservations"]:
            for inst in res["Instances"]:
                state = inst["State"]["Name"]
                if state == "terminated":
                    continue
                iid = inst["InstanceId"]
                name = next((t["Value"] for t in inst["Tags"] if t["Key"] == "Name"), "")
                ip = inst.get("PublicIpAddress", "pending")
                instance_map[iid] = {"name": name, "ip": ip, "state": state}
    except Exception as e:
        safe_exit(f"Error listing instances: {e}")

    if not instance_map:
        print("⚠️  No matching instances to terminate.")
        return

    print("\n📋 Instances to terminate:\n")
    print(tabulate([[v["name"], v["ip"], v["state"]] for v in instance_map.values()], headers=["Name", "IP", "State"]))

    confirm = input("Terminate these instances? [y/N]: ").strip().lower()
    if confirm not in ("y", "yes"):
        print("✋ Termination cancelled.")
        return

    try:
        ids = list(instance_map.keys())
        ec2.terminate_instances(InstanceIds=ids)
        for iid in ids:
            ec2_resource.Instance(iid).wait_until_terminated()
            print(f"✅ Terminated {iid} ({instance_map[iid]['name']})")
        print("🎉 All instances terminated.")
    except Exception as e:
        safe_exit(f"Failed to terminate instances: {e}")


def dump_configs(combos, out_dir):
    """Write each combo's generated args-file to out_dir for local review. No AWS."""
    os.makedirs(out_dir, exist_ok=True)
    for combo in combos:
        path = os.path.join(out_dir, f"network_params_{combo['name']}.yaml")
        with open(path, "w") as f:
            f.write(build_network_params(combo["bn"], combo["vc"]))
        print(f"📝 {path}")
    print(f"\n✅ Wrote {len(combos)} config(s) to {out_dir}/")


def init_aws_clients():
    global ec2, ec2_resource
    try:
        import boto3
        ec2 = boto3.client("ec2")
        ec2_resource = boto3.resource("ec2")
    except Exception as e:
        safe_exit(f"Failed to initialise AWS clients (is the region/credentials configured?): {e}")


def main():
    parser = argparse.ArgumentParser(description="Launch or terminate a native-Charon Kurtosis EC2 test fleet (one instance per CLxVC combo).")
    parser.add_argument("--branch", default="main", help="kurtosis-charon git branch to clone (default: main)")
    parser.add_argument("--lifetime", default="60m", help="Shutdown after time (default: 60m e.g. 90m, 2h)")
    parser.add_argument("--monitoring-token", help="PROMETHEUS_REMOTE_WRITE_TOKEN for Prometheus remote_write")
    parser.add_argument("--terminate", action="store_true", help="Terminate matching EC2 instances")
    parser.add_argument("--on-demand", action="store_true", help="Use On-Demand EC2 instances (default is Spot)")
    parser.add_argument("--instance-type", default=DEFAULT_INSTANCE_TYPE, help=f"EC2 instance type for all combos (default: {DEFAULT_INSTANCE_TYPE})")
    parser.add_argument("--only", help="Comma-separated combo names to launch (default: all 36). Format: <cl>-<vc>, e.g. lighthouse-teku,prysm-vouch")
    parser.add_argument("--dump-configs", nargs="?", const="generated_configs", metavar="DIR", help="Write all generated args-files to DIR (default: generated_configs) and exit. No AWS calls.")
    args = parser.parse_args()

    combos = COMBOS
    if args.only:
        wanted = {n.strip() for n in args.only.split(",") if n.strip()}
        unknown = wanted - {c["name"] for c in COMBOS}
        if unknown:
            safe_exit(f"Unknown combo(s): {', '.join(sorted(unknown))}. Run with --dump-configs to see all {len(COMBOS)} names.")
        combos = [c for c in COMBOS if c["name"] in wanted]

    # Local-only path: no AWS needed.
    if args.dump_configs:
        dump_configs(combos, args.dump_configs)
        return

    init_aws_clients()

    tag_values = [instance_tag(c) for c in combos]

    if args.terminate:
        terminate_instances(tag_values)
        return

    if not args.monitoring_token:
        safe_exit("Missing required --monitoring-token (unless using --terminate or --dump-configs)")

    shutdown_minutes = parse_lifetime_arg(args.lifetime)

    print(f"🔍 {len(combos)} combo(s) to launch:")
    for c in combos:
        print(f"  - {c['name']:22s} enclave={enclave_name(c):28s} [{args.instance_type}]")
    confirm = input(f"\nLaunch {len(combos)} EC2 instance(s)? [y/N]: ").strip().lower()
    if confirm not in ("y", "yes"):
        print("✋ Launch cancelled.")
        return

    ami_id = get_latest_ubuntu_ami()
    print(f"\n🚀 Launching with AMI {ami_id}, branch '{args.branch}', shutdown in {shutdown_minutes}m")
    print(f"📌 Instance type: {args.instance_type}, On-Demand: {args.on_demand}, Charon image: {CHARON_IMAGE}\n")

    launched_ids = []
    id_to_tag = {}
    for combo in combos:
        iid, tag = launch_instance(combo, ami_id, args.branch, shutdown_minutes, args.monitoring_token, args.instance_type, args.on_demand)
        if iid:
            launched_ids.append(iid)
            id_to_tag[iid] = tag

    if launched_ids:
        wait_until_running(launched_ids, id_to_tag)
        time.sleep(10)

    print("\n📦 Instance Summary:")
    print(tabulate(fetch_instance_table(tag_values), headers=["Name", "IP", "State"]))


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        safe_exit("Interrupted by user. Exiting.")
