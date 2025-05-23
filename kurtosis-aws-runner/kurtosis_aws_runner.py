import boto3
import os
import re
import sys
import time
import argparse
from botocore.exceptions import ClientError, BotoCoreError
from tabulate import tabulate

# --- Configuration Constants ---
KEY_NAME = "kurtosis-fleet"
SECURITY_GROUP_ID = "sg-0e208fd6ad761cafc"
SUBNET_ID = "subnet-07d83bab8a2b8cd7d"
DEFAULT_INSTANCE_TYPE = "c6a.xlarge"
VOLUME_SIZE = 50
VOLUME_TYPE = "gp3"
VOLUME_IOPS = 6000  # optimized for Charon test runs
VOLUME_THROUGHPUT = 250  # MB/s
BASE_TAG = "kurtosis-fleet"
DEFAULT_ENV_DIR = "../deployments/env"
GIT_REPO = "https://github.com/ObolNetwork/kurtosis-charon.git"

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


def get_combos(env_dir):
    try:
        files = os.listdir(env_dir)
    except FileNotFoundError:
        safe_exit(f"Environment directory not found: {env_dir}")

    el = sorted({f.split('_')[1].split('.')[0] for f in files if f.startswith("el_")})
    cl = sorted({f.split('_')[1].split('.')[0] for f in files if f.startswith("cl_")})
    vc = sorted({f.split('_')[1].split('.')[0] for f in files if f.startswith("vc_")})

    if not el or not cl or not vc:
        safe_exit("Missing el_*.env, cl_*.env, or vc_*.env files in the environment directory.")

    return [f"{el_client}-{cl_client}-charon-{vc_client}" for el_client in el for cl_client in cl for vc_client in vc]


def generate_user_data(combo, branch, shutdown_minutes, monitoring_token):
    return f"""#!/bin/bash
set -euxo pipefail
sleep 20

# Schedule shutdown early
nohup bash -c "sleep {shutdown_minutes}m && /sbin/shutdown -h now" >/home/ubuntu/shutdown.log 2>&1 &

apt-get update -y
apt-get install -y apt-transport-https ca-certificates curl software-properties-common git make jq gettext bash
curl -fsSL https://get.docker.com | sh
usermod -aG docker ubuntu
DOCKER_COMPOSE_VERSION=$(curl -s https://api.github.com/repos/docker/compose/releases/latest | jq -r .tag_name)
curl -L "https://github.com/docker/compose/releases/download/${{DOCKER_COMPOSE_VERSION}}/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
chmod +x /usr/local/bin/docker-compose
echo "deb [trusted=yes] https://apt.fury.io/kurtosis-tech/ /" > /etc/apt/sources.list.d/kurtosis.list
apt update -y
apt install -y kurtosis-cli

su - ubuntu <<'EOF'
cd /home/ubuntu
git clone -b {branch} {GIT_REPO}
cd kurtosis-charon
echo "PROMETHEUS_REMOTE_WRITE_TOKEN={monitoring_token}" >> deployments/env/charon.env
make {combo} || true
EOF
"""


def instance_tag(combo):
    return f"{BASE_TAG}-{combo}"


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
        print(f"❌ Failed to launch {combo}: {e}")
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
    parser = argparse.ArgumentParser(description="Launch or terminate Kurtosis EC2 test fleet.")
    parser.add_argument("--branch", default="main", help="Git branch to clone (default: main)")
    parser.add_argument("--lifetime", default="120", help="Shutdown after time (default: 120 e.g. 90m, 2h)")
    parser.add_argument("--env-dir", default=DEFAULT_ENV_DIR, help="Directory of combos .env files")
    parser.add_argument("--monitoring-token", required=True, help="Monitoring token for Prometheus remote write")
    parser.add_argument("--terminate", action="store_true", help="Terminate matching EC2 instances")
    parser.add_argument("--on-demand", action="store_true", help="Use On-Demand EC2 instances (default is Spot)")
    parser.add_argument("--instance-type", default=DEFAULT_INSTANCE_TYPE, help="EC2 instance type (default: c6a.xlarge)")
    args = parser.parse_args()

    shutdown_minutes = parse_lifetime_arg(args.lifetime)
    combos = get_combos(args.env_dir)
    tag_values = [instance_tag(c) for c in combos]

    if args.terminate:
        terminate_instances(tag_values)
        return

    print(f"🔍 Found {len(combos)} combinations:")
    for c in combos:
        print(f"  - {c}")
    confirm = input(f"\nLaunch {len(combos)} EC2 instances? [y/N]: ").strip().lower()
    if confirm not in ("y", "yes"):
        print("✋ Launch cancelled.")
        return

    ami_id = get_latest_ubuntu_ami()
    print(f"\n🚀 Launching with AMI {ami_id}, branch '{args.branch}', shutdown in {shutdown_minutes}m")
    print(f"📌 Instance type: {args.instance_type}, On-Demand: {args.on_demand}\n")

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
