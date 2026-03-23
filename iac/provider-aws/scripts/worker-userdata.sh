#!/bin/bash
# =============================================================================
# E2B Worker Node Bootstrap Script
# =============================================================================
# This script is run by cloud-init on first boot of worker EC2 instances.
# It installs and configures the Nomad client, Docker, and prepares the node
# for running Firecracker microVMs.
#
# Key requirements for Firecracker on AWS:
# - Nitro-based instance (bare metal or nested virt enabled)
# - KVM kernel module loaded
# - NBD kernel module for disk images
# - Swap + tmpfs for snapshot cache performance
# - s3fs for mounting FC binary buckets
# =============================================================================

set -euo pipefail

# -----------------------------------------------------------------------------
# Configuration (injected via Terraform templatefile)
# -----------------------------------------------------------------------------
AWS_REGION="${aws_region}"
ENVIRONMENT="${environment}"
PREFIX="${prefix}"
DOMAIN_NAME="${domain_name}"
ECR_REGISTRY="${ecr_registry}"
TEMPLATE_BUCKET="${template_bucket}"
BUILD_BUCKET="${build_bucket}"
SECRETS_PREFIX="${prefix}-${environment}"
DATACENTER="${datacenter}"

# Export for subshells and cron jobs
export AWS_REGION ENVIRONMENT PREFIX DOMAIN_NAME ECR_REGISTRY
export TEMPLATE_BUCKET BUILD_BUCKET SECRETS_PREFIX DATACENTER

# Logging — all output goes to log file, syslog, and console
exec > >(tee /var/log/e2b-bootstrap.log | logger -t e2b-bootstrap -s 2>/dev/console) 2>&1
echo "[$(date)] Starting E2B worker node bootstrap..."

# -----------------------------------------------------------------------------
# Install Dependencies
# -----------------------------------------------------------------------------
install_dependencies() {
    echo "[$(date)] Installing dependencies..."

    apt-get update -y
    DEBIAN_FRONTEND=noninteractive apt-get upgrade -y

    apt-get install -y \
        apt-transport-https \
        ca-certificates \
        curl \
        gnupg \
        lsb-release \
        jq \
        unzip \
        net-tools \
        software-properties-common \
        iptables \
        iproute2 \
        bridge-utils \
        nbd-client \
        s3fs \
        fuse

    # Install AWS CLI v2 (prefer v2 over apt awscli v1)
    if ! command -v aws &>/dev/null || ! aws --version 2>&1 | grep -q "aws-cli/2"; then
        curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o /tmp/awscliv2.zip
        unzip -q /tmp/awscliv2.zip -d /tmp
        /tmp/aws/install --update
        rm -rf /tmp/awscliv2.zip /tmp/aws
    fi

    # Install Docker
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
        | tee /etc/apt/sources.list.d/docker.list > /dev/null
    apt-get update -y
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

    systemctl enable docker
    systemctl start docker

    echo "[$(date)] Dependencies installed."
}

# -----------------------------------------------------------------------------
# Install HashiCorp Tools (Nomad, Consul)
# -----------------------------------------------------------------------------
install_hashicorp_tools() {
    echo "[$(date)] Installing HashiCorp tools..."

    curl -fsSL https://apt.releases.hashicorp.com/gpg | gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
    echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" \
        | tee /etc/apt/sources.list.d/hashicorp.list

    apt-get update -y
    apt-get install -y nomad consul

    echo "[$(date)] HashiCorp tools installed."
}

# -----------------------------------------------------------------------------
# Configure Kernel and System for Firecracker
# -----------------------------------------------------------------------------
configure_kernel() {
    echo "[$(date)] Configuring kernel for Firecracker..."

    # Sysctl tuning
    cat > /etc/sysctl.d/99-e2b-firecracker.conf <<'SYSCTL'
# E2B Firecracker networking
net.ipv4.ip_forward = 1
net.ipv4.conf.all.forwarding = 1
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1

# File descriptor limits (Firecracker needs many FDs per VM)
fs.file-max = 1000000
fs.nr_open = 1000000

# Network tuning for high connection counts
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535

# Memory-mapped areas (NBD + Firecracker)
vm.max_map_count = 1048576

# Swap tuning — prefer keeping hot pages in RAM
vm.swappiness = 10
vm.vfs_cache_pressure = 50
SYSCTL
    sysctl --system

    # Load required kernel modules
    modprobe br_netfilter
    modprobe nbd nbds_max=4096
    modprobe kvm
    modprobe kvm_amd 2>/dev/null || modprobe kvm_intel 2>/dev/null || true

    # Persist kernel modules
    cat > /etc/modules-load.d/e2b.conf <<'MODULES'
br_netfilter
nbd
kvm
kvm_amd
kvm_intel
MODULES

    # NBD module options (persist nbds_max across reboots)
    echo "options nbd nbds_max=4096" > /etc/modprobe.d/nbd.conf

    # Disable inotify for NBD devices (known performance issue)
    # https://lore.kernel.org/lkml/20220422054224.19527-1-matthew.ruffell@canonical.com/
    cat > /etc/udev/rules.d/97-nbd-device.rules <<'UDEV'
# Disable inotify watching of change events for NBD devices
ACTION=="add|change", KERNEL=="nbd*", OPTIONS:="nowatch"
UDEV
    udevadm control --reload-rules
    udevadm trigger

    # Increase ulimits system-wide
    cat > /etc/security/limits.d/99-e2b.conf <<'LIMITS'
* soft nofile 1048576
* hard nofile 1048576
* soft nproc 65535
* hard nproc 65535
LIMITS

    # Also set for current session
    ulimit -n 1048576 2>/dev/null || true

    echo "[$(date)] Kernel configured for Firecracker."
}

# -----------------------------------------------------------------------------
# Configure Swap and tmpfs (critical for snapshot performance)
# -----------------------------------------------------------------------------
configure_swap_and_tmpfs() {
    echo "[$(date)] Configuring swap and tmpfs..."

    # Create swap file — 100G provides headroom for snapshot operations.
    # Firecracker memory snapshots are written to disk during pause;
    # swap ensures the host doesn't OOM during concurrent snapshot ops.
    SWAPFILE="/swapfile"
    if [ ! -f "$SWAPFILE" ]; then
        fallocate -l 100G "$SWAPFILE"
        chmod 600 "$SWAPFILE"
        mkswap "$SWAPFILE"
        swapon "$SWAPFILE"
        echo "$SWAPFILE none swap sw 0 0" >> /etc/fstab
    fi

    # tmpfs for snapshot cache — keeps hot snapshot data in RAM
    # for fast resume. 65G is sized for ~50 concurrent sandbox snapshots.
    mkdir -p /mnt/snapshot-cache
    mount -t tmpfs -o size=65G tmpfs /mnt/snapshot-cache 2>/dev/null || true

    # Directories for Firecracker operations
    mkdir -p /fc-vm
    mkdir -p /orchestrator/sandbox
    mkdir -p /orchestrator/template
    mkdir -p /orchestrator/build

    echo "[$(date)] Swap and tmpfs configured."
}

# -----------------------------------------------------------------------------
# Install Firecracker
# -----------------------------------------------------------------------------
install_firecracker() {
    echo "[$(date)] Installing Firecracker..."

    FC_VERSION="v1.10.1"
    ARCH=$(uname -m)

    curl -L -o /tmp/firecracker.tgz \
        "https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-${ARCH}.tgz"

    tar -xzf /tmp/firecracker.tgz -C /tmp
    mv "/tmp/release-${FC_VERSION}-${ARCH}/firecracker-${FC_VERSION}-${ARCH}" /usr/local/bin/firecracker
    mv "/tmp/release-${FC_VERSION}-${ARCH}/jailer-${FC_VERSION}-${ARCH}" /usr/local/bin/jailer
    chmod +x /usr/local/bin/firecracker /usr/local/bin/jailer
    rm -rf /tmp/firecracker.tgz "/tmp/release-${FC_VERSION}-${ARCH}"

    /usr/local/bin/firecracker --version
    echo "[$(date)] Firecracker ${FC_VERSION} installed."
}

# -----------------------------------------------------------------------------
# Mount S3 Buckets via s3fs (for FC binaries and templates)
# -----------------------------------------------------------------------------
mount_s3_buckets() {
    echo "[$(date)] Mounting S3 buckets via s3fs..."

    # Templates bucket
    if [ -n "$TEMPLATE_BUCKET" ]; then
        mkdir -p /fc-templates
        s3fs "$TEMPLATE_BUCKET" /fc-templates \
            -o allow_other -o umask=000 -o nonempty \
            -o iam_role -o enable_noobj_cache \
            -o url="https://s3.${AWS_REGION}.amazonaws.com" \
            2>/dev/null || echo "[$(date)] Warning: Could not mount templates bucket"
    fi

    # Build artifacts bucket (kernels, FC versions, envd)
    if [ -n "$BUILD_BUCKET" ]; then
        mkdir -p /fc-builds
        s3fs "$BUILD_BUCKET" /fc-builds \
            -o allow_other -o umask=000 -o nonempty \
            -o iam_role -o enable_noobj_cache \
            -o url="https://s3.${AWS_REGION}.amazonaws.com" \
            2>/dev/null || echo "[$(date)] Warning: Could not mount builds bucket"
    fi

    echo "[$(date)] S3 buckets mounted."
}

# -----------------------------------------------------------------------------
# Fetch Secrets from AWS Secrets Manager
# -----------------------------------------------------------------------------
fetch_secrets() {
    echo "[$(date)] Fetching secrets from AWS Secrets Manager..."

    REDIS_URL=$(aws secretsmanager get-secret-value \
        --region "$AWS_REGION" \
        --secret-id "$SECRETS_PREFIX/redis-url" \
        --query 'SecretString' --output text 2>/dev/null | jq -r '.url // .' 2>/dev/null || echo "")
    export REDIS_URL

    CONSUL_TOKEN=$(aws secretsmanager get-secret-value \
        --region "$AWS_REGION" \
        --secret-id "$SECRETS_PREFIX/consul-acl-token" \
        --query 'SecretString' --output text 2>/dev/null || echo "")
    export CONSUL_TOKEN

    echo "[$(date)] Secrets fetched."
}

# -----------------------------------------------------------------------------
# Configure Consul Client
# -----------------------------------------------------------------------------
configure_consul() {
    echo "[$(date)] Configuring Consul client..."

    INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
    PRIVATE_IP=$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)

    mkdir -p /etc/consul.d /opt/consul

    cat > /etc/consul.d/consul.hcl <<EOF
datacenter = "$DATACENTER"
data_dir = "/opt/consul"
log_level = "INFO"

bind_addr = "$PRIVATE_IP"
client_addr = "0.0.0.0"

# Client mode — joins the server cluster via AWS tag discovery
server = false

retry_join = ["provider=aws tag_key=consul-cluster tag_value=$PREFIX-$ENVIRONMENT"]

performance {
  raft_multiplier = 1
}

telemetry {
  prometheus_retention_time = "24h"
  disable_hostname = true
}
EOF

    chown -R consul:consul /opt/consul 2>/dev/null || true

    systemctl enable consul
    systemctl start consul

    echo "[$(date)] Consul client configured and started."
}

# -----------------------------------------------------------------------------
# Configure Nomad Client
# -----------------------------------------------------------------------------
configure_nomad() {
    echo "[$(date)] Configuring Nomad client..."

    INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
    PRIVATE_IP=$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)
    INSTANCE_TYPE=$(curl -s http://169.254.169.254/latest/meta-data/instance-type)

    mkdir -p /etc/nomad.d /opt/nomad /var/lib/firecracker

    cat > /etc/nomad.d/nomad.hcl <<EOF
datacenter = "$DATACENTER"
data_dir = "/opt/nomad"
log_level = "INFO"

bind_addr = "0.0.0.0"
name = "$INSTANCE_ID"

server {
  enabled = false
}

client {
  enabled = true

  # Server discovery via Consul (preferred) or AWS tag-based retry
  server_join {
    retry_join = ["provider=aws tag_key=nomad-server tag_value=$PREFIX-$ENVIRONMENT"]
  }

  node_pool = "workers"

  meta {
    "node_type"       = "worker"
    "instance_type"   = "$INSTANCE_TYPE"
    "has_firecracker" = "true"
  }

  host_volume "docker-sock" {
    path      = "/var/run/docker.sock"
    read_only = false
  }

  host_volume "firecracker" {
    path      = "/var/lib/firecracker"
    read_only = false
  }

  host_volume "fc-vm" {
    path      = "/fc-vm"
    read_only = false
  }

  host_volume "snapshot-cache" {
    path      = "/mnt/snapshot-cache"
    read_only = false
  }

  # Reserve resources for OS + Nomad + Consul overhead
  reserved {
    cpu    = 500
    memory = 1024
  }
}

consul {
  address = "127.0.0.1:8500"
}

telemetry {
  collection_interval        = "1s"
  disable_hostname           = true
  prometheus_metrics         = true
  publish_allocation_metrics = true
  publish_node_metrics       = true
}

plugin "docker" {
  config {
    allow_privileged = true
    volumes {
      enabled = true
    }
    gc {
      image       = true
      image_delay = "3m"
      container   = true
    }
  }
}

plugin "raw_exec" {
  config {
    enabled = true
  }
}
EOF

    systemctl enable nomad
    systemctl start nomad

    echo "[$(date)] Nomad client configured and started."
}

# -----------------------------------------------------------------------------
# Configure ECR Authentication (with cron-safe env)
# -----------------------------------------------------------------------------
configure_ecr() {
    echo "[$(date)] Configuring ECR authentication..."

    # Write a self-contained script that doesn't depend on env vars
    cat > /usr/local/bin/ecr-login.sh <<ECREOF
#!/bin/bash
set -e
AWS_REGION="$AWS_REGION"
ECR_REGISTRY="$ECR_REGISTRY"
aws ecr get-login-password --region "\$AWS_REGION" | docker login --username AWS --password-stdin "\$ECR_REGISTRY"
ECREOF
    chmod +x /usr/local/bin/ecr-login.sh

    # Initial login
    /usr/local/bin/ecr-login.sh

    # Refresh ECR credentials every 6 hours (token expires every 12h)
    echo "0 */6 * * * root /usr/local/bin/ecr-login.sh >> /var/log/ecr-login.log 2>&1" > /etc/cron.d/ecr-refresh

    echo "[$(date)] ECR authentication configured."
}

# -----------------------------------------------------------------------------
# Download E2B Orchestrator Binary
# -----------------------------------------------------------------------------
download_orchestrator() {
    echo "[$(date)] Downloading E2B orchestrator..."

    mkdir -p /opt/e2b/bin

    aws s3 cp "s3://$BUILD_BUCKET/orchestrator" /opt/e2b/bin/orchestrator --region "$AWS_REGION" 2>/dev/null || {
        echo "[$(date)] Warning: Could not download orchestrator from S3. Will be deployed via Nomad job."
    }
    chmod +x /opt/e2b/bin/orchestrator 2>/dev/null || true

    echo "[$(date)] E2B orchestrator download attempted."
}

# -----------------------------------------------------------------------------
# Install CloudWatch Agent
# -----------------------------------------------------------------------------
install_cloudwatch_agent() {
    echo "[$(date)] Installing CloudWatch agent..."

    ARCH=$(dpkg --print-architecture)
    wget -q "https://s3.amazonaws.com/amazoncloudwatch-agent/ubuntu/${ARCH}/latest/amazon-cloudwatch-agent.deb" -O /tmp/cwagent.deb
    dpkg -i /tmp/cwagent.deb
    rm /tmp/cwagent.deb

    cat > /opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json <<CWEOF
{
  "agent": {
    "metrics_collection_interval": 60,
    "run_as_user": "root"
  },
  "logs": {
    "logs_collected": {
      "files": {
        "collect_list": [
          {
            "file_path": "/var/log/e2b-bootstrap.log",
            "log_group_name": "/e2b/$ENVIRONMENT/bootstrap",
            "log_stream_name": "{instance_id}"
          },
          {
            "file_path": "/var/log/nomad/*.log",
            "log_group_name": "/e2b/$ENVIRONMENT/nomad",
            "log_stream_name": "{instance_id}"
          },
          {
            "file_path": "/var/log/consul/*.log",
            "log_group_name": "/e2b/$ENVIRONMENT/consul",
            "log_stream_name": "{instance_id}"
          },
          {
            "file_path": "/var/lib/firecracker/**/*.log",
            "log_group_name": "/e2b/$ENVIRONMENT/orchestrator",
            "log_stream_name": "{instance_id}"
          }
        ]
      }
    }
  },
  "metrics": {
    "namespace": "E2B/$ENVIRONMENT",
    "metrics_collected": {
      "cpu": {
        "measurement": ["cpu_usage_idle", "cpu_usage_user", "cpu_usage_system"],
        "metrics_collection_interval": 60
      },
      "mem": {
        "measurement": ["mem_used_percent", "mem_available_percent"],
        "metrics_collection_interval": 60
      },
      "disk": {
        "measurement": ["disk_used_percent"],
        "metrics_collection_interval": 60,
        "resources": ["/", "/var/lib/firecracker", "/mnt/snapshot-cache"]
      },
      "swap": {
        "measurement": ["swap_used_percent"],
        "metrics_collection_interval": 60
      }
    }
  }
}
CWEOF

    /opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
        -a fetch-config -m ec2 \
        -c file:/opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json \
        -s

    echo "[$(date)] CloudWatch agent installed and configured."
}

# -----------------------------------------------------------------------------
# Tag Instance for Discovery
# -----------------------------------------------------------------------------
tag_instance() {
    echo "[$(date)] Tagging instance for discovery..."

    INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)

    aws ec2 create-tags --region "$AWS_REGION" --resources "$INSTANCE_ID" --tags \
        "Key=consul-cluster,Value=$PREFIX-$ENVIRONMENT" \
        "Key=nomad-client,Value=$PREFIX-$ENVIRONMENT" \
        "Key=e2b-role,Value=worker"

    echo "[$(date)] Instance tagged."
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
main() {
    echo "[$(date)] =========================================="
    echo "[$(date)] E2B Worker Node Bootstrap Starting"
    echo "[$(date)] Environment: $ENVIRONMENT"
    echo "[$(date)] Region: $AWS_REGION"
    echo "[$(date)] =========================================="

    install_dependencies
    install_hashicorp_tools
    configure_kernel
    configure_swap_and_tmpfs
    install_firecracker
    fetch_secrets
    tag_instance
    configure_consul
    configure_nomad
    configure_ecr
    mount_s3_buckets
    download_orchestrator
    install_cloudwatch_agent

    echo "[$(date)] =========================================="
    echo "[$(date)] E2B Worker Node Bootstrap Complete!"
    echo "[$(date)] =========================================="
}

main
