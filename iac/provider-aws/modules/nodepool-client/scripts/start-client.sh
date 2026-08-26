#!/usr/bin/env bash

# This script is meant to be run in the User Data of each EC2 Instance while it's booting. The script uses the
# run-nomad and run-consul scripts to configure and start Nomad and Consul in client mode. Note that this script
# assumes it's running in an AMI built from the Packer template in examples/nomad-consul-ami/nomad-consul.json.

set -euo pipefail

# Set timestamp format
PS4='[\D{%Y-%m-%d %H:%M:%S}] '
# Enable command tracing
set -x

# Send the log output from this script to user-data.log, syslog, and the console
# Inspired by https://alestic.com/2010/12/ec2-user-data-output/
exec > >(tee /var/log/user-data.log | logger -t user-data -s 2>/dev/console) 2>&1

mkdir -p /orchestrator
mkdir -p /orchestrator/sandbox
mkdir -p /orchestrator/template
mkdir -p /orchestrator/build

# Add swapfile
SWAPFILE="/swapfile"
fallocate -l 100G $SWAPFILE
chmod 600 $SWAPFILE
mkswap $SWAPFILE
swapon $SWAPFILE

# Make swapfile persistent
echo "$SWAPFILE none swap sw 0 0" | tee -a /etc/fstab

# Set swap settings
sysctl vm.swappiness=10
sysctl vm.vfs_cache_pressure=50

# Add tmpfs for snapshotting
# TODO: Parametrize this
mkdir -p /mnt/snapshot-cache
mount -t tmpfs -o size=65G tmpfs /mnt/snapshot-cache

ulimit -n 1048576
export GOMAXPROCS=$(nproc)

tee -a /etc/sysctl.conf <<EOF
# Increase the maximum number of socket connections
net.core.somaxconn = 65535

# Increase the maximum number of backlogged connections
net.core.netdev_max_backlog = 65535

# Increase maximum number of TCP sockets
net.ipv4.tcp_max_syn_backlog = 65535

# Increase the maximum number of memory map areas
vm.max_map_count=1048576

EOF
sysctl -p

echo "Configuring NBD capacity"
# The AMI may have loaded nbd with the kernel default before user-data runs.
# Persist the desired capacity and reload the module when it is still unused;
# silently accepting a smaller capacity makes the orchestrator's pool appear
# healthy while reducing the number of concurrent Firecracker sandboxes.
cat <<EOF >/etc/modprobe.d/e2b-nbd.conf
options nbd nbds_max=${NBD_MAX_DEVICES}
EOF

current_nbd_max="$(cat /sys/module/nbd/parameters/nbds_max 2>/dev/null || echo 0)"
if [ "$current_nbd_max" -gt 0 ] && [ "$current_nbd_max" -lt "${NBD_MAX_DEVICES}" ]; then
  if ! modprobe -r nbd 2>/dev/null; then
    echo "ERROR: nbd is already loaded at $${current_nbd_max}; cannot raise capacity to ${NBD_MAX_DEVICES} safely"
    exit 1
  fi
fi

# Disable inotify for NBD devices.
# https://lore.kernel.org/lkml/20220422054224.19527-1-matthew.ruffell@canonical.com/
cat <<EOH >/etc/udev/rules.d/97-nbd-device.rules
# Disable inotify watching of change events for NBD devices
ACTION=="add|change", KERNEL=="nbd*", OPTIONS:="nowatch"
EOH

udevadm control --reload-rules
udevadm trigger

# Load the NBD module with the declared capacity and fail closed if the
# kernel did not honor it. The orchestrator must never run with an unknown
# sandbox-device ceiling.
modprobe nbd nbds_max="${NBD_MAX_DEVICES}"
actual_nbd_max="$(cat /sys/module/nbd/parameters/nbds_max)"
if [ "$actual_nbd_max" -lt "${NBD_MAX_DEVICES}" ]; then
  echo "ERROR: nbd capacity is $${actual_nbd_max}; expected at least ${NBD_MAX_DEVICES}"
  exit 1
fi

# Create the directory for the fc mounts
mkdir -p /fc-vm

# Mount envd buckets
envd_dir="/fc-envd"
mkdir -p $envd_dir
s3fs "${FC_ENV_PIPELINE_BUCKET_NAME}" "$envd_dir" -o allow_other -o umask=000 -o nonempty -o iam_role -o enable_noobj_cache

# Mount kernels
kernels_dir="/fc-kernels"
mkdir -p $kernels_dir
s3fs "${FC_KERNELS_BUCKET_NAME}" "$kernels_dir" -o allow_other -o umask=000 -o nonempty -o iam_role -o enable_noobj_cache

# Mount FC versions
fc_versions_dir="/fc-versions"
mkdir -p $fc_versions_dir
s3fs "${FC_VERSIONS_BUCKET_NAME}" "$fc_versions_dir" -o allow_other -o umask=000 -o nonempty -o iam_role -o enable_noobj_cache

# Mount busybox
busybox_dir="/fc-busybox"
mkdir -p $busybox_dir
s3fs "${FC_BUSYBOX_BUCKET_NAME}" "$busybox_dir" -o allow_other -o umask=000 -o nonempty -o iam_role -o enable_noobj_cache

# These variables are passed in via Terraform template interpolation
aws s3 cp "s3://${SCRIPTS_BUCKET}/run-consul-${RUN_CONSUL_FILE_HASH}.sh" /opt/consul/bin/run-consul.sh
aws s3 cp "s3://${SCRIPTS_BUCKET}/run-nomad-${RUN_NOMAD_FILE_HASH}.sh" /opt/nomad/bin/run-nomad.sh

chmod +x /opt/consul/bin/run-consul.sh /opt/nomad/bin/run-nomad.sh

# Resolve bootstrap credentials at boot from the instance role. Never embed
# ACL tokens or gossip keys in launch-template user-data.
# xtrace is enabled above for bootstrap diagnostics, but must be disabled while
# resolving and assigning secret values so they cannot land in user-data.log.
set +x
command -v aws >/dev/null
command -v jq >/dev/null
cluster_secret_json="$(aws secretsmanager get-secret-value --secret-id "${CLUSTER_SECRET_ARN}" --query SecretString --output text)"
CONSUL_TOKEN="$(jq -er '.CONSUL_ACL_TOKEN' <<<"$${cluster_secret_json}")"
NOMAD_ACL_TOKEN="$(jq -er '.NOMAD_ACL_TOKEN' <<<"$${cluster_secret_json}")"
CONSUL_DNS_REQUEST_TOKEN="$(jq -er '.CONSUL_DNS_REQUEST_TOKEN' <<<"$${cluster_secret_json}")"
CONSUL_GOSSIP_ENCRYPTION_KEY="$(jq -er '.CONSUL_GOSSIP_ENCRYPTION_KEY' <<<"$${cluster_secret_json}")"
unset cluster_secret_json
set -x

mkdir -p /root/docker
touch /root/docker/config.json
cat <<EOF >/root/docker/config.json
{
    "credHelpers": {
        "${AWS_ECR_ACCOUNT_REPOSITORY_DOMAIN}": "ecr-login"
    }
}
EOF

mkdir -p /etc/systemd/resolved.conf.d/
touch /etc/systemd/resolved.conf.d/consul.conf
cat <<EOF >/etc/systemd/resolved.conf.d/consul.conf
[Resolve]
DNS=127.0.0.1#8600
DNSSEC=false
DNSStubListener=yes
DNSStubListenerExtra=172.17.0.1
EOF
sync  # Ensure file is written to disk

# Set up huge pages
# We are not enabling Transparent Huge Pages for now, as they are not swappable and may result in slowdowns + we are not using swap right now.
# The THP are by default set to madvise
# We are allocating the hugepages at the start when the memory is not fragmented yet
echo "[Setting up huge pages]"
mkdir -p /mnt/hugepages
mount -t hugetlbfs none /mnt/hugepages
# Increase proactive compaction to reduce memory fragmentation for using overcomitted huge pages

available_ram=$(grep MemTotal /proc/meminfo | awk '{print $2}') # in KiB
available_ram=$(($available_ram / 1024))                        # in MiB
echo "- Total memory: $available_ram MiB"

min_normal_ram=$((4 * 1024))                             # 4 GiB
min_normal_percentage_ram=$(($available_ram * 16 / 100)) # 16% of the total memory
max_normal_ram=$((42 * 1024))                            # 42 GiB

max() {
    if (($1 > $2)); then
        echo "$1"
    else
        echo "$2"
    fi
}

min() {
    if (($1 < $2)); then
        echo "$1"
    else
        echo "$2"
    fi
}

ensure_even() {
    if (($1 % 2 == 0)); then
        echo "$1"
    else
        echo $(($1 - 1))
    fi
}

remove_decimal() {
    echo "$(echo $1 | sed 's/\..*//')"
}

reserved_normal_ram=$(max $min_normal_ram $min_normal_percentage_ram)
reserved_normal_ram=$(min $reserved_normal_ram $max_normal_ram)
echo "- Reserved RAM: $reserved_normal_ram MiB"

# The huge pages RAM should still be usable for normal pages in most cases.
hugepages_ram=$(($available_ram - $reserved_normal_ram))
hugepages_ram=$(remove_decimal $hugepages_ram)
hugepages_ram=$(ensure_even $hugepages_ram)
echo "- RAM for hugepages: $hugepages_ram MiB"

hugepage_size_in_mib=2
echo "- Huge page size: $hugepage_size_in_mib MiB"
hugepages=$(($hugepages_ram / $hugepage_size_in_mib))

# This percentage will be permanently allocated for huge pages and in monitoring it will be shown as used.
base_hugepages_percentage=${BASE_HUGEPAGES_PERCENTAGE}
base_hugepages=$(($hugepages * $base_hugepages_percentage / 100))
base_hugepages=$(remove_decimal $base_hugepages)
echo "- Allocating $base_hugepages huge pages ($base_hugepages_percentage%) for base usage"
echo $base_hugepages >/proc/sys/vm/nr_hugepages

overcommitment_hugepages_percentage=$((100 - $base_hugepages_percentage))
overcommitment_hugepages=$(($hugepages * $overcommitment_hugepages_percentage / 100))
overcommitment_hugepages=$(remove_decimal $overcommitment_hugepages)
echo "- Allocating $overcommitment_hugepages huge pages ($overcommitment_hugepages_percentage%) for overcommitment"
echo $overcommitment_hugepages >/proc/sys/vm/nr_overcommit_hugepages

# Start Consul first (in background) with GCE DNS as recursor
# This allows Consul to handle both .consul queries AND forward internet queries
# These variables are passed in via Terraform template interpolation
/opt/consul/bin/run-consul.sh --client \
    --consul-token "$${CONSUL_TOKEN}" \
    --cluster-tag-name "${CLUSTER_TAG_NAME}" \
    --cluster-tag-value "${CLUSTER_TAG_VALUE}"  \
    --enable-gossip-encryption \
    --gossip-encryption-key "$${CONSUL_GOSSIP_ENCRYPTION_KEY}" \
    --dns-request-token "$${CONSUL_DNS_REQUEST_TOKEN}" &

# Give Consul a moment to start its DNS server on port 8600
echo "- Waiting for Consul DNS to start on port 8600..."
for i in {1..60}; do
  if (echo >/dev/tcp/127.0.0.1/8600) 2>/dev/null; then
    echo "- Consul DNS is ready (attempt $i/60)"
    break
  fi
  if [ $i -eq 60 ]; then
    echo "- ERROR: Consul DNS not responding after 60 seconds, exiting..."
    exit 1
  fi
  sleep 1
done

# Now restart systemd-resolved to apply Consul DNS configuration
# This must happen AFTER Consul starts, otherwise systemd-resolved marks 127.0.0.1:8600 as unreachable
# Consul DNS (127.0.0.1:8600) is the ONLY DNS server configured in systemd-resolved
# Consul handles ALL queries: .consul directly, everything else via recursor to GCE DNS
echo "[Configuring systemd-resolved for Consul DNS]"
echo "- Restarting systemd-resolved to apply Consul DNS config"
systemctl restart systemd-resolved
echo "- Waiting for systemd-resolved to settle"

# Give Consul a moment to start its DNS server on port 8600
echo "- Waiting for Systemd-resolved to start..."
for i in {1..60}; do
  if host google.com 2>/dev/null; then
    echo "- DNS resolving is ready (attempt $i/60)"
    break
  fi
  if [ $i -eq 60 ]; then
    echo "- ERROR: Systemd-resolved not responding after 60 seconds, exiting..."
    exit 1
  fi
  sleep 1
done
echo "- Flushing DNS caches"
resolvectl flush-caches

set +x
/opt/nomad/bin/run-nomad.sh --client --consul-token "$${CONSUL_TOKEN}" --nomad-token "$${NOMAD_ACL_TOKEN}" --node-pool "${NODE_POOL}" --node-type "${NODE_TYPE}" --node-labels "${NODE_LABELS}" &
set -x

# Do not report cloud-init success until the local Nomad client is serving.
# This bounded gate leaves actionable console/journal evidence on failure;
# server-side node registration is checked by the deployment acceptance probe.
echo "- Waiting for local Nomad client health..."
nomad_health_ready="false"
for i in {1..60}; do
  if curl --silent --show-error --fail --max-time 2 http://127.0.0.1:4646/v1/agent/health >/dev/null 2>&1; then
    nomad_health_ready="true"
    echo "- Nomad client health is ready (attempt $i/60)"
    break
  fi
  sleep 1
done

if [[ "$nomad_health_ready" != "true" ]]; then
  echo "- ERROR: Nomad client did not become healthy after 60 seconds"
  supervisorctl status nomad || true
  for log_file in /opt/nomad/log/*; do
    [[ -f "$log_file" ]] || continue
    echo "--- $log_file (tail) ---"
    tail -n 80 "$log_file" || true
  done
  exit 1
fi

# A listening local agent is necessary but not sufficient: the client must
# complete its RPC registration with a Nomad server before this node can run
# work. Keep this bounded and leave the rendered config plus supervisor logs
# in the console when the registration contract is not met.
echo "- Waiting for Nomad server registration..."
nomad_registration_ready="false"
set +x
metadata_token=$(curl --silent --show-error --fail --connect-timeout 2 --max-time 5 \
  --request PUT --header 'X-aws-ec2-metadata-token-ttl-seconds: 21600' \
  http://169.254.169.254/latest/api/token)
instance_id=$(curl --silent --show-error --fail --header "X-aws-ec2-metadata-token: $metadata_token" \
  http://169.254.169.254/latest/meta-data/instance-id)
for i in {1..60}; do
  if [ -s /run/nomad-server-addresses ]; then
    while IFS= read -r server_address; do
      server_http_address="http://$${server_address%:4647}:4646"
      if curl --silent --show-error --fail --max-time 2 \
        --header "X-Nomad-Token: $${NOMAD_ACL_TOKEN}" \
        "$server_http_address/v1/nodes" | grep -q "\"Name\":\"$${instance_id}\""; then
        nomad_registration_ready="true"
        echo "- Nomad server registration is ready (attempt $i/60)"
        break 2
      fi
    done < /run/nomad-server-addresses
  fi
  if [ "$nomad_registration_ready" = "true" ]; then
    break
  fi
  sleep 1
done
set -x
if [ "$nomad_registration_ready" != "true" ]; then
  echo "- ERROR: Nomad client did not register with a server after 60 seconds"
  echo "- Rendered client configuration:"
  sed -n '/^client {/,/^}/p' /opt/nomad/config/default.hcl || true
  supervisorctl status nomad || true
  tail -n 120 /var/log/nomad/* 2>/dev/null || true
  exit 1
fi

# Add alias for ssh-ing to sbx
echo '_sbx_ssh() {
  local address=$(dig @127.0.0.4 $1. A +short 2>/dev/null)
  ssh -o StrictHostKeyChecking=accept-new "root@$address"
}

alias sbx-ssh=_sbx_ssh' >>/etc/profile
