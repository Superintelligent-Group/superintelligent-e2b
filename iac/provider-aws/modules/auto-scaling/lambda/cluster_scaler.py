"""
E2B Cluster Auto-Scaler Lambda

Two entry points:
  - wake_handler:     Scales up control + API + client ASGs (spot preferred),
                      then re-evaluates all Nomad jobs so dead/pending ones
                      reschedule onto the new nodes.
  - shutdown_handler:  Checks for idle and scales worker nodes to zero.

The wake handler is called via Lambda Function URL by the swarm worker
before it needs a sandbox. The shutdown handler runs on a 5-minute
EventBridge schedule.
"""

import json
import os
import ssl
import time
import urllib.request
import urllib.error

import boto3

autoscaling = boto3.client("autoscaling")
ec2 = boto3.client("ec2")
cloudwatch = boto3.client("cloudwatch")
secretsmanager = boto3.client("secretsmanager")

CONTROL_ASG = os.environ["CONTROL_SERVER_ASG_NAME"]
API_ASG = os.environ["API_ASG_NAME"]
CLIENT_ASG = os.environ["CLIENT_ASG_NAME"]
BUILD_ASG = os.environ["BUILD_ASG_NAME"]
IDLE_TIMEOUT = int(os.environ.get("IDLE_TIMEOUT_MINUTES", "30"))
SPOT_INSTANCE_TYPES = json.loads(os.environ.get("CLIENT_SPOT_INSTANCE_TYPES", "[]"))
API_SPOT_INSTANCE_TYPES = json.loads(os.environ.get("API_SPOT_INSTANCE_TYPES", "[]"))

# Nomad/Consul secrets for job re-evaluation after scale-up
NOMAD_TOKEN_SECRET = os.environ.get("NOMAD_TOKEN_SECRET_ID", "")
CONSUL_TOKEN_SECRET = os.environ.get("CONSUL_TOKEN_SECRET_ID", "")
NOMAD_ADDR = os.environ.get("NOMAD_ADDR", "")

ACTIVITY_METRIC_NAMESPACE = "E2B/AutoScaling"
ACTIVITY_METRIC_NAME = "LastActivityTimestamp"

# Cache secrets in Lambda execution environment (warm starts)
_secret_cache = {}


def _get_secret(secret_id):
    """Retrieve a secret from Secrets Manager with caching."""
    if not secret_id:
        return ""
    if secret_id in _secret_cache:
        return _secret_cache[secret_id]
    try:
        resp = secretsmanager.get_secret_value(SecretId=secret_id)
        val = resp["SecretString"].strip()
        _secret_cache[secret_id] = val
        return val
    except Exception as e:
        print(f"WARN: failed to get secret {secret_id}: {e}")
        return ""


# --------------- ASG helpers ---------------


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


# --------------- CloudWatch activity metric ---------------


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


# --------------- Instance health ---------------


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


# --------------- Nomad job re-evaluation ---------------

# TLS context that skips cert verification for internal Nomad API
_tls_ctx = ssl.create_default_context()
_tls_ctx.check_hostname = False
_tls_ctx.verify_mode = ssl.CERT_NONE


def _nomad_request(method, path, body=None):
    """Make a request to the Nomad HTTP API."""
    nomad_token = _get_secret(NOMAD_TOKEN_SECRET)
    if not NOMAD_ADDR or not nomad_token:
        print("WARN: NOMAD_ADDR or token not configured, skipping Nomad API call")
        return None

    url = f"{NOMAD_ADDR}{path}"
    headers = {
        "X-Nomad-Token": nomad_token,
        "Content-Type": "application/json",
    }
    data = json.dumps(body).encode() if body else None

    try:
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        with urllib.request.urlopen(req, timeout=30, context=_tls_ctx) as resp:
            return json.loads(resp.read().decode())
    except Exception as e:
        print(f"WARN: Nomad API {method} {path} failed: {e}")
        return None


def _wait_for_nomad_nodes(expected_pools, timeout_seconds=120):
    """Wait for Nomad to register nodes in the expected pools."""
    start = time.time()
    while time.time() - start < timeout_seconds:
        nodes = _nomad_request("GET", "/v1/nodes")
        if nodes:
            ready_pools = set()
            for n in nodes:
                if n.get("Status") == "ready":
                    ready_pools.add(n.get("NodePool", "default"))
            if expected_pools.issubset(ready_pools):
                print(f"Nomad nodes ready in pools: {ready_pools}")
                return True
        time.sleep(10)
    print(f"WARN: Timed out waiting for Nomad nodes in pools {expected_pools}")
    return False


def _resubmit_nomad_jobs():
    """Resubmit all Nomad jobs so dead/pending ones reschedule on new nodes.

    After a scale-to-zero cycle, old allocations become 'lost' and jobs
    go 'dead'. A simple evaluate doesn't recover them — we need to
    resubmit the full job spec with the Consul token.
    """
    consul_token = _get_secret(CONSUL_TOKEN_SECRET)
    jobs = _nomad_request("GET", "/v1/jobs")
    if not jobs:
        print("WARN: Could not list Nomad jobs")
        return

    resubmitted = []
    for job_summary in jobs:
        name = job_summary["Name"]
        status = job_summary["Status"]

        # Only resubmit dead/pending jobs (running ones are fine)
        if status == "running":
            continue

        # Fetch full job spec
        job = _nomad_request("GET", f"/v1/job/{name}")
        if not job:
            continue

        # Strip read-only fields for resubmission
        for key in [
            "Status", "StatusDescription", "Stable", "SubmitTime",
            "Version", "CreateIndex", "ModifyIndex", "JobModifyIndex",
        ]:
            job.pop(key, None)

        # Attach Consul token for service registration
        if consul_token:
            job["ConsulToken"] = consul_token

        result = _nomad_request("POST", "/v1/jobs", {"Job": job})
        if result and "EvalID" in result:
            resubmitted.append(name)
            print(f"Resubmitted job '{name}' (was {status}) → eval {result['EvalID'][:8]}")
        else:
            print(f"WARN: Failed to resubmit job '{name}': {result}")

    if resubmitted:
        print(f"Resubmitted {len(resubmitted)} jobs: {resubmitted}")
    else:
        print("No jobs needed resubmission")


# --------------- Handlers ---------------


def wake_handler(event, context):
    """Scale up the cluster. Called by swarm worker before sandbox creation."""

    # Record activity FIRST — prevents the shutdown handler from racing us
    # during the long scale-up process (CloudWatch metric propagation delay).
    _record_activity()

    # Check if already up
    if _is_asg_up(API_ASG) and _is_asg_up(CLIENT_ASG):
        return {
            "statusCode": 200,
            "body": json.dumps({"status": "already_running"}),
        }

    print("Cluster not running — starting scale-up")

    # Ensure control server is up (should always be, but just in case)
    if not _is_asg_up(CONTROL_ASG):
        print("Control server down — scaling up")
        _scale_asg(CONTROL_ASG, 1)  # Control plane on-demand for reliability
        _wait_for_healthy(CONTROL_ASG, timeout_seconds=240)

    # Scale up API + Client in parallel, using spot
    for asg_name, spot_types, max_sz in [
        (API_ASG, API_SPOT_INSTANCE_TYPES, 2),
        (CLIENT_ASG, SPOT_INSTANCE_TYPES, 3),
    ]:
        try:
            _scale_asg_spot(asg_name, 1, spot_types, max_size=max_sz)
            print(f"Scaled up {asg_name} (spot)")
        except Exception as e:
            print(f"WARN: spot scaling failed for {asg_name}, falling back to on-demand: {e}")
            _scale_asg(asg_name, 1, max_size=max_sz)

    # Wait for API node to be healthy first (Nomad jobs need it)
    api_ready = _wait_for_healthy(API_ASG, timeout_seconds=300)
    print(f"API node healthy: {api_ready}")

    # Wait for client to be healthy (that's what we need for sandboxes)
    client_ready = _wait_for_healthy(CLIENT_ASG, timeout_seconds=300)
    print(f"Client node healthy: {client_ready}")

    # Wait for Nomad to register the new nodes
    _wait_for_nomad_nodes({"api", "default"}, timeout_seconds=120)

    # Resubmit dead/pending Nomad jobs so they schedule on new nodes
    _resubmit_nomad_jobs()

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

    # Boot grace: if any ASG instance launched recently, skip shutdown.
    # This prevents the shutdown handler from racing the wake handler
    # when CloudWatch metric propagation is slow.
    BOOT_GRACE_MINUTES = 10
    for asg_name in [API_ASG, CLIENT_ASG]:
        asg = _get_asg(asg_name)
        if asg:
            for inst in asg.get("Instances", []):
                if inst["LifecycleState"] in ("Pending", "Pending:Wait", "Pending:Proceed"):
                    print(f"Instance {inst['InstanceId']} still booting in {asg_name} — skipping shutdown")
                    return {"statusCode": 200, "body": json.dumps({"status": "booting"})}

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
        print(f"Cluster active — idle {idle_minutes:.1f} min (timeout: {IDLE_TIMEOUT} min)")
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

    print(f"Cluster idle {idle_minutes:.1f} min — shutting down worker nodes")

    # Scale worker nodes to zero, but keep control server running.
    # Keep max_size > 0 so the wake handler can scale back up without
    # needing to update max_size first (avoids race conditions).
    for asg_name in [CLIENT_ASG, BUILD_ASG, API_ASG]:
        _scale_asg(asg_name, 0)

    return {
        "statusCode": 200,
        "body": json.dumps(
            {"status": "shutdown", "idle_minutes": round(idle_minutes, 1)}
        ),
    }
