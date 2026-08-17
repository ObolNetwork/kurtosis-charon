import boto3
import re
import sys
import time
import argparse
from tabulate import tabulate

KEY_NAME = "kurtosis-fleet"
SECURITY_GROUP_ID = "sg-0e208fd6ad761cafc"
SUBNET_ID = "subnet-000b1456766381ae9"  # eu-west-1c
DEFAULT_INSTANCE_TYPE = "c6a.8xlarge"
VOLUME_SIZE = 50
VOLUME_TYPE = "gp3"
VOLUME_IOPS = 6000  # optimized for Charon test runs
VOLUME_THROUGHPUT = 250  # MB/s
BASE_TAG = "kurtosis-fleet"
GIT_REPO = "https://github.com/ObolNetwork/kurtosis-charon.git"
NETWORK_PARAMS_DIR = "network-params"
ETHEREUM_PACKAGE = "github.com/ObolNetwork/ethereum-package@charon"
KURTOSIS_DEB_URL = "https://github.com/kurtosis-tech/kurtosis-cli-release-artifacts/releases/download/1.20.0/kurtosis-cli_1.20.0_linux_amd64.deb"

# All 36 CL x VC combos. Shares args-files with the DV cycler (network-params/).
_BNS = ["lighthouse", "lodestar", "nimbus", "teku", "prysm", "grandine"]
_VCS = ["lighthouse", "lodestar", "nimbus", "teku", "prysm", "vouch"]
COMBOS = [
    {
        "name": f"{bn}-{vc}",
        "bn": bn,
        "vc": vc,
        "file": f"{bn}-{vc}.yaml",
    }
    for bn in _BNS
    for vc in _VCS
]


def enclave_name(combo):
    return f"{combo['bn']}-charon-{combo['vc']}"


ec2 = boto3.client("ec2")
ec2_resource = boto3.resource("ec2")


def safe_exit(message):
    print(f"❌ {message}")
    sys.exit(1)


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


def generate_user_data(combo, branch, shutdown_minutes, monitoring_token, charon_version):
    """Cloud-init that installs Docker + Kurtosis, clones kurtosis-charon, then runs
    the native-Charon kurtosis command (mirrors `make run-native`) against this
    combo's split args-file. $PROMETHEUS_REMOTE_WRITE_TOKEN and $CHARON_VERSION
    placeholders in the args-file are substituted with envsubst before the run.
    """
    args_file = f"{NETWORK_PARAMS_DIR}/{combo['file']}"
    enclave = enclave_name(combo)
    return f"""#!/bin/bash
set -euxo pipefail
sleep 20

# Schedule shutdown early so the instance always self-terminates.
nohup bash -c "sleep {shutdown_minutes}m && /sbin/shutdown -h now" >/home/ubuntu/shutdown.log 2>&1 &

apt-get update -y
apt-get install -y apt-transport-https ca-certificates curl software-properties-common git make jq gettext bash
curl -fsSL https://get.docker.com | sh
usermod -aG docker ubuntu
curl -fsSL -o /tmp/kurtosis-cli.deb {KURTOSIS_DEB_URL}
dpkg -i /tmp/kurtosis-cli.deb

su - ubuntu <<'EOF'
cd /home/ubuntu
git clone -b {branch} {GIT_REPO}
cd kurtosis-charon
export PROMETHEUS_REMOTE_WRITE_TOKEN="{monitoring_token}"
export CHARON_VERSION="{charon_version}"
envsubst '$PROMETHEUS_REMOTE_WRITE_TOKEN $CHARON_VERSION' < {args_file} > /tmp/network_params.yaml
kurtosis run --enclave {enclave} {ETHEREUM_PACKAGE} --args-file /tmp/network_params.yaml || true
EOF
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


def launch_instance(combo, ami_id, branch, shutdown_minutes, monitoring_token, charon_version, instance_type, on_demand):
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
        "UserData": generate_user_data(combo, branch, shutdown_minutes, monitoring_token, charon_version),
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


def main():
    parser = argparse.ArgumentParser(description="Launch or terminate a native-Charon Kurtosis EC2 test fleet (one instance per combo).")
    parser.add_argument("--branch", default="main", help="kurtosis-charon git branch to clone (default: main)")
    parser.add_argument("--lifetime", default="60m", help="Shutdown after time (default: 60m e.g. 90m, 2h)")
    parser.add_argument("--monitoring-token", help="PROMETHEUS_REMOTE_WRITE_TOKEN for Prometheus remote_write")
    parser.add_argument("--charon-version", required=False, help="Charon image tag to substitute into args-files (e.g. v1.11.0)")
    parser.add_argument("--terminate", action="store_true", help="Terminate matching EC2 instances")
    parser.add_argument("--on-demand", action="store_true", help="Use On-Demand EC2 instances (default is Spot)")
    parser.add_argument("--instance-type", default=DEFAULT_INSTANCE_TYPE, help=f"EC2 instance type for all combos (default: {DEFAULT_INSTANCE_TYPE})")
    parser.add_argument("--only", help="Comma-separated filter: combo names (lighthouse-prysm), cl:<client> for all combos with that CL, vc:<client> for all combos with that VC")
    args = parser.parse_args()

    combos = COMBOS
    if args.only:
        filters = [f.strip() for f in args.only.split(",") if f.strip()]
        matched = []
        for f in filters:
            if f.startswith("cl:"):
                cl = f[3:]
                batch = [c for c in COMBOS if c["bn"] == cl]
                if not batch:
                    safe_exit(f"Unknown CL '{cl}'. Options: {', '.join(_BNS)}")
                matched.extend(batch)
            elif f.startswith("vc:"):
                vc = f[3:]
                batch = [c for c in COMBOS if c["vc"] == vc]
                if not batch:
                    safe_exit(f"Unknown VC '{vc}'. Options: {', '.join(_VCS)}")
                matched.extend(batch)
            else:
                batch = [c for c in COMBOS if c["name"] == f]
                if not batch:
                    safe_exit(f"Unknown combo '{f}'. Use <cl>-<vc>, cl:<client>, or vc:<client>")
                matched.extend(batch)
        seen = set()
        combos = [c for c in matched if c["name"] not in seen and not seen.add(c["name"])]

    tag_values = [instance_tag(c) for c in combos]

    if args.terminate:
        terminate_instances(tag_values)
        return

    if not args.monitoring_token:
        safe_exit("Missing required --monitoring-token (unless using --terminate)")
    if not args.charon_version:
        safe_exit("Missing required --charon-version (e.g. v1.11.0)")

    shutdown_minutes = parse_lifetime_arg(args.lifetime)

    print(f"🔍 {len(combos)} combo(s) to launch:")
    for c in combos:
        print(f"  - {c['name']:16s} {c['file']:42s} [{args.instance_type}]")
    confirm = input(f"\nLaunch {len(combos)} EC2 instance(s)? [y/N]: ").strip().lower()
    if confirm not in ("y", "yes"):
        print("✋ Launch cancelled.")
        return

    ami_id = get_latest_ubuntu_ami()
    print(f"\n🚀 Launching with AMI {ami_id}, branch '{args.branch}', shutdown in {shutdown_minutes}m")
    print(f"📌 Instance type: {args.instance_type}, On-Demand: {args.on_demand}, Charon: {args.charon_version}\n")

    launched_ids = []
    id_to_tag = {}
    for combo in combos:
        iid, tag = launch_instance(combo, ami_id, args.branch, shutdown_minutes, args.monitoring_token, args.charon_version, args.instance_type, args.on_demand)
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
