"""
E2B Cluster Auto-Scaler Lambda

Two entry points:
  - wake_handler:     Scales up control + API + client ASGs (spot preferred)
  - shutdown_handler:  Checks for idle and scales everything to zero

The wake handler is called via Lambda Function URL by the swarm worker
before it needs a sandbox. The shutdown handler runs on a 5-minute
EventBridge schedule.
"""

import json
import os
import time
import boto3

autoscaling = boto3.client("autoscaling")
ec2 = boto3.client("ec2")
cloudwatch = boto3.client("cloudwatch")

CONTROL_ASG = os.environ["CONTROL_SERVER_ASG_NAME"]
API_ASG = os.environ["API_ASG_NAME"]
CLIENT_ASG = os.environ["CLIENT_ASG_NAME"]
BUILD_ASG = os.environ["BUILD_ASG_NAME"]
IDLE_TIMEOUT = int(os.environ.get("IDLE_TIMEOUT_MINUTES", "30"))
SPOT_INSTANCE_TYPES = json.loads(os.environ.get("CLIENT_SPOT_INSTANCE_TYPES", "[]"))
API_SPOT_INSTANCE_TYPES = json.loads(os.environ.get("API_SPOT_INSTANCE_TYPES", "[]"))

ACTIVITY_METRIC_NAMESPACE = "E2B/AutoScaling"
ACTIVITY_METRIC_NAME = "LastActivityTimestamp"


def _get_asg(name):
    resp = autoscaling.describe_auto_scaling_groups(AutoScalingGroupNames=[name])
    groups = resp.get("AutoScalingGroups", [])
    return groups[0] if groups else None


def _is_asg_up(name):
    asg = _get_asg(name)
    return asg and asg["DesiredCapacity"] > 0


def _scale_asg(name, desired, max_size=None):
    if max_size is None:
        max_size = max(desired, 1)
    autoscaling.update_auto_scaling_group(
        AutoScalingGroupName=name,
        MinSize=0,
        MaxSize=max_size,
        DesiredCapacity=desired,
    )


def _scale_asg_spot(name, desired, spot_types, max_size=None):
    """Scale ASG with spot mixed instances policy for cost savings."""
    if max_size is None:
        max_size = max(desired, 1)

    asg = _get_asg(name)
    if not asg or not spot_types:
        _scale_asg(name, desired, max_size)
        return

    lt = asg.get("LaunchTemplate") or asg.get("MixedInstancesPolicy", {}).get(
        "LaunchTemplate", {}
    ).get("LaunchTemplateSpecification", {})

    if not lt:
        _scale_asg(name, desired, max_size)
        return

    lt_id = lt.get("LaunchTemplateId", lt.get("launchTemplateId"))
    lt_version = lt.get("Version", lt.get("version", "$Latest"))

    overrides = [{"InstanceType": t} for t in spot_types]

    autoscaling.update_auto_scaling_group(
        AutoScalingGroupName=name,
        MinSize=0,
        MaxSize=max_size,
        DesiredCapacity=desired,
        MixedInstancesPolicy={
            "LaunchTemplate": {
                "LaunchTemplateSpecification": {
                    "LaunchTemplateId": lt_id,
                    "Version": lt_version,
                },
                "Overrides": overrides,
            },
            "InstancesDistribution": {
                "OnDemandBaseCapacity": 0,
                "OnDemandPercentageAboveBaseCapacity": 0,  # 100% spot
                "SpotAllocationStrategy": "capacity-optimized",
            },
        },
    )


def _record_activity():
    cloudwatch.put_metric_data(
        Namespace=ACTIVITY_METRIC_NAMESPACE,
        MetricData=[
            {
                "MetricName": ACTIVITY_METRIC_NAME,
                "Value": time.time(),
                "Unit": "Seconds",
            }
        ],
    )


def _get_last_activity():
    resp = cloudwatch.get_metric_statistics(
        Namespace=ACTIVITY_METRIC_NAMESPACE,
        MetricName=ACTIVITY_METRIC_NAME,
        StartTime=time.time() - 7200,  # last 2 hours
        EndTime=time.time(),
        Period=300,
        Statistics=["Maximum"],
    )
    datapoints = resp.get("Datapoints", [])
    if not datapoints:
        return 0
    return max(dp["Maximum"] for dp in datapoints)


def _wait_for_healthy(asg_name, timeout_seconds=300):
    """Wait for at least one healthy instance in the ASG."""
    start = time.time()
    while time.time() - start < timeout_seconds:
        asg = _get_asg(asg_name)
        if asg:
            healthy = [
                i
                for i in asg.get("Instances", [])
                if i["LifecycleState"] == "InService"
            ]
            if healthy:
                return True
        time.sleep(10)
    return False


def wake_handler(event, context):
    """Scale up the cluster. Called by swarm worker before sandbox creation."""

    # Check if already up (control server is always running)
    if _is_asg_up(API_ASG) and _is_asg_up(CLIENT_ASG):
        _record_activity()
        return {
            "statusCode": 200,
            "body": json.dumps({"status": "already_running"}),
        }

    # Ensure control server is up (should always be, but just in case)
    if not _is_asg_up(CONTROL_ASG):
        _scale_asg(CONTROL_ASG, 1)  # Control plane on-demand for reliability
        _wait_for_healthy(CONTROL_ASG, timeout_seconds=240)

    # Scale up API + Client in parallel, using spot
    for asg_name, spot_types, max_sz in [
        (API_ASG, API_SPOT_INSTANCE_TYPES, 2),
        (CLIENT_ASG, SPOT_INSTANCE_TYPES, 3),
    ]:
        try:
            _scale_asg_spot(asg_name, 1, spot_types, max_size=max_sz)
        except Exception as e:
            print(f"WARN: spot scaling failed for {asg_name}, falling back to on-demand: {e}")
            _scale_asg(asg_name, 1, max_size=max_sz)

    # Wait for client to be healthy (that's what we need for sandboxes)
    client_ready = _wait_for_healthy(CLIENT_ASG, timeout_seconds=300)

    _record_activity()

    return {
        "statusCode": 200 if client_ready else 202,
        "body": json.dumps(
            {
                "status": "ready" if client_ready else "starting",
                "message": "Cluster is ready"
                if client_ready
                else "Cluster is starting, may take a few more minutes",
            }
        ),
    }


def shutdown_handler(event, context):
    """Check for idle cluster and scale to zero. Runs every 5 minutes."""

    # If worker nodes already scaled down, nothing to do
    # (Control server stays up to preserve Nomad state)
    if not _is_asg_up(API_ASG) and not _is_asg_up(CLIENT_ASG):
        return {"statusCode": 200, "body": json.dumps({"status": "already_down"})}

    # Check last activity
    last_activity = _get_last_activity()
    if last_activity == 0:
        # No activity data found — CloudWatch metric may not have propagated yet.
        # Treat as recently active to avoid premature shutdown.
        print("No activity metric found — assuming recently started, skipping shutdown")
        _record_activity()  # Seed the metric so next check has data
        return {
            "statusCode": 200,
            "body": json.dumps({"status": "active", "reason": "no_metric_yet"}),
        }
    idle_seconds = time.time() - last_activity
    idle_minutes = idle_seconds / 60

    if idle_minutes < IDLE_TIMEOUT:
        return {
            "statusCode": 200,
            "body": json.dumps(
                {
                    "status": "active",
                    "idle_minutes": round(idle_minutes, 1),
                    "timeout": IDLE_TIMEOUT,
                }
            ),
        }

    # Scale worker nodes to zero, but keep control server running
    # (Nomad server state is not persistent across instance terminations,
    #  so we keep the control server alive to preserve job definitions)
    for asg_name in [CLIENT_ASG, BUILD_ASG, API_ASG]:
        _scale_asg(asg_name, 0, max_size=0)

    return {
        "statusCode": 200,
        "body": json.dumps(
            {"status": "shutdown", "idle_minutes": round(idle_minutes, 1)}
        ),
    }
