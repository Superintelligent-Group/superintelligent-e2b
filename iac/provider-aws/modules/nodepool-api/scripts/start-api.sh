#!/usr/bin/env bash

set -euo pipefail

# Set timestamp format
PS4='[\D{%Y-%m-%d %H:%M:%S}] '
# Enable command tracing
set -x

# Send the log output from this script to user-data.log, syslog, and the console
# Inspired by https://alestic.com/2010/12/ec2-user-data-output/
exec > >(tee /var/log/user-data.log | logger -t user-data -s 2>/dev/console) 2>&1

ulimit -n 1048576
export GOMAXPROCS=$(nproc)

# --------------- Persistent Postgres EBS Volume ---------------
# Attach, format (if needed), and mount the EBS volume for Postgres data.
# This survives scale-to-zero cycles so DB state persists.

POSTGRES_EBS_VOLUME_ID="${POSTGRES_EBS_VOLUME_ID}"
POSTGRES_MOUNT_POINT="/opt/e2b-postgres-data"
EBS_DEVICE="/dev/xvdf"
EBS_ATTACHED=false

if [ -n "$POSTGRES_EBS_VOLUME_ID" ]; then
    echo "Attaching EBS volume $POSTGRES_EBS_VOLUME_ID..."

    # Get instance ID from metadata
    IMDS_TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
    INSTANCE_ID=$(curl -s -H "X-aws-ec2-metadata-token: $IMDS_TOKEN" http://169.254.169.254/latest/meta-data/instance-id)
    AWS_REGION=$(curl -s -H "X-aws-ec2-metadata-token: $IMDS_TOKEN" http://169.254.169.254/latest/meta-data/placement/region)

    # Wait for volume to be available (may still be attached/detaching from a
    # just-terminated instance -- AWS's own detach can lag well past the
    # terminate call, especially under frequent scale-to-zero churn).
    for i in $(seq 1 30); do
        VOL_STATE=$(aws ec2 describe-volumes --volume-ids "$POSTGRES_EBS_VOLUME_ID" \
            --region "$AWS_REGION" --query 'Volumes[0].State' --output text 2>/dev/null || echo "unknown")
        if [ "$VOL_STATE" = "available" ]; then
            break
        fi
        if [ "$VOL_STATE" = "in-use" ]; then
            # Force detach from previous (terminated) instance
            ATTACHED_INSTANCE=$(aws ec2 describe-volumes --volume-ids "$POSTGRES_EBS_VOLUME_ID" \
                --region "$AWS_REGION" --query 'Volumes[0].Attachments[0].InstanceId' --output text 2>/dev/null)
            echo "Volume attached to $ATTACHED_INSTANCE, force-detaching..."
            aws ec2 detach-volume --volume-id "$POSTGRES_EBS_VOLUME_ID" --force --region "$AWS_REGION" 2>/dev/null || true
        fi
        echo "Waiting for volume ($VOL_STATE)... attempt $i/30"
        sleep 5
    done

    # Attach the volume, retrying: a single one-shot attempt right after the
    # state-wait loop above races AWS's own detach-completion bookkeeping --
    # the volume can report "available" and still reject an immediate attach
    # (VolumeInUse/IncorrectState) for a few more seconds.
    for i in $(seq 1 12); do
        ATTACH_ERR=$(aws ec2 attach-volume \
            --volume-id "$POSTGRES_EBS_VOLUME_ID" \
            --instance-id "$INSTANCE_ID" \
            --device "$EBS_DEVICE" \
            --region "$AWS_REGION" 2>&1) && { EBS_ATTACHED=true; break; }
        echo "attach-volume attempt $i/12 failed, retrying: $ATTACH_ERR"
        sleep 5
    done

    if [ "$EBS_ATTACHED" = true ]; then
        # Wait for the device to appear
        for i in $(seq 1 30); do
            if [ -b "$EBS_DEVICE" ] || [ -b "/dev/nvme1n1" ]; then
                break
            fi
            echo "Waiting for block device... attempt $i/30"
            sleep 2
        done

        # On Nitro instances, /dev/xvdf may appear as /dev/nvme1n1
        ACTUAL_DEVICE="$EBS_DEVICE"
        if [ -b "/dev/nvme1n1" ] && [ ! -b "$EBS_DEVICE" ]; then
            ACTUAL_DEVICE="/dev/nvme1n1"
        fi
    fi

    # Only format/mount if the device really showed up. Falling through to
    # mkfs on a device that never appeared used to hard-crash this script
    # under `set -e` -- which killed Nomad client startup below along with
    # it, taking the whole node pool down for what should be a soft,
    # ephemeral-storage-for-this-boot degradation, not a missing node.
    if [ "$EBS_ATTACHED" = true ] && [ -b "$ACTUAL_DEVICE" ]; then
        # Format if no filesystem exists
        if ! blkid "$ACTUAL_DEVICE" >/dev/null 2>&1; then
            echo "Formatting $ACTUAL_DEVICE with ext4..."
            mkfs.ext4 -L e2b-pgdata "$ACTUAL_DEVICE"
        fi

        # Mount
        mkdir -p "$POSTGRES_MOUNT_POINT"
        mount "$ACTUAL_DEVICE" "$POSTGRES_MOUNT_POINT"
        echo "Mounted $ACTUAL_DEVICE at $POSTGRES_MOUNT_POINT"

        # Ensure postgres user (UID 70 in alpine) can write
        chown -R 70:70 "$POSTGRES_MOUNT_POINT" 2>/dev/null || true
    else
        echo "WARN: EBS volume never attached/appeared as a block device -- falling back to ephemeral storage for this boot"
        mkdir -p "$POSTGRES_MOUNT_POINT"
    fi
else
    echo "No POSTGRES_EBS_VOLUME_ID configured, using ephemeral storage"
    mkdir -p "$POSTGRES_MOUNT_POINT"
fi

sudo tee -a /etc/sysctl.conf <<EOF
# Increase the maximum number of socket connections
net.core.somaxconn = 65535

# Increase the maximum number of backlogged connections
net.core.netdev_max_backlog = 65535

# Increase maximum number of TCP sockets
net.ipv4.tcp_max_syn_backlog = 65535
EOF
sudo sysctl -p

# These variables are passed in via Terraform template interpolation
aws s3 cp "s3://${SCRIPTS_BUCKET}/run-consul-${RUN_CONSUL_FILE_HASH}.sh" /opt/consul/bin/run-consul.sh
aws s3 cp "s3://${SCRIPTS_BUCKET}/run-nomad-${RUN_NOMAD_FILE_HASH}.sh" /opt/nomad/bin/run-nomad.sh

chmod +x /opt/consul/bin/run-consul.sh /opt/nomad/bin/run-nomad.sh

mkdir -p /root/docker
touch /root/docker/config.json
cat <<EOF >/root/docker/config.json
{
    "credHelpers": {
        "${AWS_ECR_ACCOUNT_REPOSITORY_DOMAIN}": "ecr-login"
    }
}
EOF

# --------------- Pre-pull Docker images ---------------
# Pull images that Nomad jobs will need immediately on startup.
# This avoids 20-60s delays when batch jobs (like DB seed) run
# on a fresh node that hasn't cached the image yet.
echo "Pre-pulling Docker images..."
docker pull postgres:15-alpine &
DOCKER_PULL_PID=$!

mkdir -p /etc/systemd/resolved.conf.d/
touch /etc/systemd/resolved.conf.d/consul.conf
cat <<EOF >/etc/systemd/resolved.conf.d/consul.conf
[Resolve]
DNS=127.0.0.1#8600
DNSSEC=false
Domains=~consul
DNSStubListener=yes
DNSStubListenerExtra=172.17.0.1
EOF


# These variables are passed in via Terraform template interpolation
/opt/consul/bin/run-consul.sh --client \
    --consul-token "${CONSUL_TOKEN}" \
    --cluster-tag-name "${CLUSTER_TAG_NAME}" \
    --cluster-tag-value "${CLUSTER_TAG_VALUE}" \
    --enable-gossip-encryption \
    --gossip-encryption-key "${CONSUL_GOSSIP_ENCRYPTION_KEY}" \
    --dns-request-token "${CONSUL_DNS_REQUEST_TOKEN}" &

# systemd-resolved must be restarted only after Consul's DNS listener exists.
# Restarting it before the background Consul process is listening causes the
# resolver to mark 127.0.0.1:8600 unreachable; API tasks then fail closed on
# redis.service.consul even though the Consul catalog is healthy.
echo "Waiting for Consul DNS to start on port 8600..."
for i in {1..60}; do
    if nc -z 127.0.0.1 8600 2>/dev/null; then
        echo "Consul DNS is ready (attempt $i/60)"
        break
    fi
    if [ "$i" -eq 60 ]; then
        echo "ERROR: Consul DNS not responding after 60 seconds, exiting..."
        exit 1
    fi
    sleep 1
done
systemctl restart systemd-resolved
resolvectl flush-caches

/opt/nomad/bin/run-nomad.sh --client --consul-token "${CONSUL_TOKEN}" --node-pool "${NODE_POOL}" &

# Wait for background Docker pre-pull to finish (non-blocking if already done)
# The doubled dollar escapes bash brace-expansion so templatefile() emits it
# literally; unescaped, Terraform reads it as an interpolation and plan fails.
if [ -n "$${DOCKER_PULL_PID:-}" ]; then
    wait "$DOCKER_PULL_PID" 2>/dev/null && echo "Docker pre-pull complete" || echo "Docker pre-pull finished (may have had warnings)"
fi
