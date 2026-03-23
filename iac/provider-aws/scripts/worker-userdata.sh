#!/bin/bash
# =============================================================================
# E2B Worker Node Bootstrap Script
# =============================================================================
# This script is run by cloud-init on first boot of worker EC2 instances.
# It installs and configures the Nomad client, Docker, and prepares the node
# for running Firecracker microVMs.
# =============================================================================

set -euo pipefail

# -----------------------------------------------------------------------------
# Configuration (injected via Terraform templatefile)
# -----------------------------------------------------------------------------
export AWS_REGION="${aws_region}"
export ENVIRONMENT="${environment}"
export PREFIX="${prefix}"
export DOMAIN_NAME="${domain_name}"

# ECR Repository
export ECR_REGISTRY="${ecr_registry}"

# S3 Buckets
export TEMPLATE_BUCKET="${template_bucket}"
export BUILD_BUCKET="${build_bucket}"

# Secrets Manager paths
export SECRETS_PREFIX="${prefix}-${environment}"

# Nomad/Consul configuration
export DATACENTER="${datacenter}"

# Logging
exec > >(tee /var/log/e2b-bootstrap.log|logger -t e2b-bootstrap -s 2>/dev/console) 2>&1
echo "[$(date)] Starting E2B worker node bootstrap..."

# -----------------------------------------------------------------------------
# Install Dependencies
# -----------------------------------------------------------------------------
install_dependencies() {
    echo "[$(date)] Installing dependencies..."

    # Update system
    apt-get update -y
    apt-get upgrade -y

    # Install required packages
    apt-get install -y \
        apt-transport-https \
        ca-certificates \
        curl \
        gnupg \
        lsb-release \
        jq \
        unzip \
        awscli \
        net-tools \
        software-properties-common \
        iptables \
        iproute2 \
        bridge-utils \
        nbd-client

    # Install Docker
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
    apt-get update -y
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

    # Enable and start Docker
    systemctl enable docker
    systemctl start docker

    echo "[$(date)] Dependencies installed."
}

# -----------------------------------------------------------------------------
# Install HashiCorp Tools (Nomad, Consul)
# -----------------------------------------------------------------------------
install_hashicorp_tools() {
    echo "[$(date)] Installing HashiCorp tools..."

    # Add HashiCorp GPG key
    curl -fsSL https://apt.releases.hashicorp.com/gpg | gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
    echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | tee /etc/apt/sources.list.d/hashicorp.list

    apt-get update -y
    apt-get install -y nomad consul

    echo "[$(date)] HashiCorp tools installed."
}

# -----------------------------------------------------------------------------
# Configure Kernel for Firecracker
# -----------------------------------------------------------------------------
configure_kernel() {
    echo "[$(date)] Configuring kernel for Firecracker..."

    # Enable IP forwarding
    cat >> /etc/sysctl.conf <<EOF
# E2B Firecracker networking
net.ipv4.ip_forward = 1
net.ipv4.conf.all.forwarding = 1
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1

# Increase file descriptor limits
fs.file-max = 1000000
fs.nr_open = 1000000

# Network tuning for high connection counts
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535
EOF

    sysctl -p

    # Load required kernel modules
    modprobe br_netfilter
    modprobe nbd max_part=16
    modprobe kvm
    modprobe kvm_amd 2>/dev/null || modprobe kvm_intel 2>/dev/null || true

    # Persist kernel modules
    cat > /etc/modules-load.d/e2b.conf <<EOF
br_netfilter
nbd
kvm
kvm_amd
kvm_intel
EOF

    # Set up hugepages for Firecracker (optional, improves performance)
    echo 1024 > /proc/sys/vm/nr_hugepages 2>/dev/null || true

    # Increase ulimits
    cat >> /etc/security/limits.conf <<EOF
* soft nofile 1000000
* hard nofile 1000000
* soft nproc 65535
* hard nproc 65535
EOF

    echo "[$(date)] Kernel configured for Firecracker."
}

# -----------------------------------------------------------------------------
# Install Firecracker
# -----------------------------------------------------------------------------
install_firecracker() {
    echo "[$(date)] Installing Firecracker..."

    # Download Firecracker (get latest stable version)
    FC_VERSION="v1.5.0"
    ARCH=$(uname -m)

    curl -L -o /tmp/firecracker.tgz \
        "https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-${ARCH}.tgz"

    tar -xzf /tmp/firecracker.tgz -C /tmp
    mv /tmp/release-${FC_VERSION}-${ARCH}/firecracker-${FC_VERSION}-${ARCH} /usr/local/bin/firecracker
    mv /tmp/release-${FC_VERSION}-${ARCH}/jailer-${FC_VERSION}-${ARCH} /usr/local/bin/jailer
    chmod +x /usr/local/bin/firecracker /usr/local/bin/jailer
    rm -rf /tmp/firecracker.tgz /tmp/release-${FC_VERSION}-${ARCH}

    # Verify installation
    /usr/local/bin/firecracker --version

    echo "[$(date)] Firecracker installed."
}

# -----------------------------------------------------------------------------
# Fetch Secrets from AWS Secrets Manager
# -----------------------------------------------------------------------------
fetch_secrets() {
    echo "[$(date)] Fetching secrets from AWS Secrets Manager..."

    # Redis URL
    export REDIS_URL=$(aws secretsmanager get-secret-value \
        --region $AWS_REGION \
        --secret-id "$SECRETS_PREFIX/redis-url" \
        --query 'SecretString' --output text | jq -r '.url // .')

    # Consul ACL token
    export CONSUL_TOKEN=$(aws secretsmanager get-secret-value \
        --region $AWS_REGION \
        --secret-id "$SECRETS_PREFIX/consul-acl-token" \
        --query 'SecretString' --output text 2>/dev/null || echo "")

    # ClickHouse connection
    export CLICKHOUSE_CONNECTION=$(aws secretsmanager get-secret-value \
        --region $AWS_REGION \
        --secret-id "$SECRETS_PREFIX/clickhouse" \
        --query 'SecretString' --output text 2>/dev/null | jq -r '.connection_string // ""')

    echo "[$(date)] Secrets fetched."
}

# -----------------------------------------------------------------------------
# Configure Consul Client
# -----------------------------------------------------------------------------
configure_consul() {
    echo "[$(date)] Configuring Consul client..."

    INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
    PRIVATE_IP=$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)

    mkdir -p /etc/consul.d

    cat > /etc/consul.d/consul.hcl <<EOF
datacenter = "$DATACENTER"
data_dir = "/opt/consul"
log_level = "INFO"

bind_addr = "$PRIVATE_IP"
client_addr = "0.0.0.0"

# Client mode
server = false

# Auto-join via AWS tags
retry_join = ["provider=aws tag_key=consul-cluster tag_value=$PREFIX-$ENVIRONMENT"]

# Performance tuning
performance {
  raft_multiplier = 1
}

# Telemetry
telemetry {
  prometheus_retention_time = "24h"
  disable_hostname = true
}
EOF

    mkdir -p /opt/consul
    chown -R consul:consul /opt/consul

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

    mkdir -p /etc/nomad.d

    cat > /etc/nomad.d/nomad.hcl <<EOF
datacenter = "$DATACENTER"
data_dir = "/opt/nomad"
log_level = "INFO"

bind_addr = "0.0.0.0"

name = "$INSTANCE_ID"

# Client-only configuration
server {
  enabled = false
}

client {
  enabled = true

  # Server discovery
  servers = ["provider=aws tag_key=nomad-server tag_value=$PREFIX-$ENVIRONMENT"]

  # Node pool for workers
  node_pool = "workers"

  # Node metadata
  meta {
    "node_type"      = "worker"
    "instance_type"  = "$INSTANCE_TYPE"
    "has_firecracker" = "true"
  }

  # Host volumes
  host_volume "docker-sock" {
    path = "/var/run/docker.sock"
    read_only = false
  }

  host_volume "firecracker" {
    path = "/var/lib/firecracker"
    read_only = false
  }

  # Resource reservation for system processes
  reserved {
    cpu = 500
    memory = 512
  }
}

# Consul integration
consul {
  address = "127.0.0.1:8500"
}

# Telemetry
telemetry {
  collection_interval = "1s"
  disable_hostname = true
  prometheus_metrics = true
  publish_allocation_metrics = true
  publish_node_metrics = true
}

# Plugin configuration
plugin "docker" {
  config {
    allow_privileged = true
    volumes {
      enabled = true
    }
    gc {
      image = true
      image_delay = "3m"
      container = true
    }
  }
}

plugin "raw_exec" {
  config {
    enabled = true
  }
}
EOF

    # Create directories
    mkdir -p /opt/nomad
    mkdir -p /var/lib/firecracker

    systemctl enable nomad
    systemctl start nomad

    echo "[$(date)] Nomad client configured and started."
}

# -----------------------------------------------------------------------------
# Configure ECR Authentication
# -----------------------------------------------------------------------------
configure_ecr() {
    echo "[$(date)] Configuring ECR authentication..."

    cat > /usr/local/bin/ecr-login.sh <<'ECREOF'
#!/bin/bash
aws ecr get-login-password --region $AWS_REGION | docker login --username AWS --password-stdin $ECR_REGISTRY
ECREOF
    chmod +x /usr/local/bin/ecr-login.sh

    /usr/local/bin/ecr-login.sh

    echo "0 */6 * * * root /usr/local/bin/ecr-login.sh" > /etc/cron.d/ecr-refresh

    echo "[$(date)] ECR authentication configured."
}

# -----------------------------------------------------------------------------
# Download E2B Orchestrator Binary
# -----------------------------------------------------------------------------
download_orchestrator() {
    echo "[$(date)] Downloading E2B orchestrator..."

    mkdir -p /opt/e2b/bin

    # Download orchestrator from S3
    aws s3 cp "s3://$BUILD_BUCKET/orchestrator" /opt/e2b/bin/orchestrator --region $AWS_REGION || {
        echo "[$(date)] Warning: Could not download orchestrator from S3. It may need to be deployed via Nomad job."
    }

    chmod +x /opt/e2b/bin/orchestrator 2>/dev/null || true

    echo "[$(date)] E2B orchestrator download attempted."
}

# -----------------------------------------------------------------------------
# Install CloudWatch Agent
# -----------------------------------------------------------------------------
install_cloudwatch_agent() {
    echo "[$(date)] Installing CloudWatch agent..."

    wget -q https://s3.amazonaws.com/amazoncloudwatch-agent/ubuntu/amd64/latest/amazon-cloudwatch-agent.deb
    dpkg -i amazon-cloudwatch-agent.deb
    rm amazon-cloudwatch-agent.deb

    cat > /opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json <<EOF
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
        "measurement": ["mem_used_percent"],
        "metrics_collection_interval": 60
      },
      "disk": {
        "measurement": ["disk_used_percent"],
        "metrics_collection_interval": 60,
        "resources": ["/", "/var/lib/firecracker"]
      }
    }
  }
}
EOF

    /opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
        -a fetch-config \
        -m ec2 \
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

    aws ec2 create-tags --region $AWS_REGION --resources $INSTANCE_ID --tags \
        Key=consul-cluster,Value=$PREFIX-$ENVIRONMENT \
        Key=nomad-client,Value=$PREFIX-$ENVIRONMENT \
        Key=e2b-role,Value=worker

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
    install_firecracker
    fetch_secrets
    tag_instance
    configure_consul
    configure_nomad
    configure_ecr
    download_orchestrator
    install_cloudwatch_agent

    echo "[$(date)] =========================================="
    echo "[$(date)] E2B Worker Node Bootstrap Complete!"
    echo "[$(date)] =========================================="
}

main
