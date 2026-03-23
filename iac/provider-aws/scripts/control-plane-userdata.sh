#!/bin/bash
# =============================================================================
# E2B Control Plane Node Bootstrap Script
# =============================================================================
# This script is run by cloud-init on first boot of control plane EC2 instances.
# It installs and configures Nomad, Consul, Docker, and pulls E2B services.
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
SECRETS_PREFIX="${prefix}-${environment}"
DATACENTER="${datacenter}"
NOMAD_SERVER_COUNT="${nomad_server_count}"
CONSUL_SERVER_COUNT="${consul_server_count}"

export AWS_REGION ENVIRONMENT PREFIX DOMAIN_NAME ECR_REGISTRY
export SECRETS_PREFIX DATACENTER NOMAD_SERVER_COUNT CONSUL_SERVER_COUNT

# Logging
exec > >(tee /var/log/e2b-bootstrap.log|logger -t e2b-bootstrap -s 2>/dev/console) 2>&1
echo "[$(date)] Starting E2B control plane bootstrap..."

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
        software-properties-common

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
# Fetch Secrets from AWS Secrets Manager
# -----------------------------------------------------------------------------
fetch_secrets() {
    echo "[$(date)] Fetching secrets from AWS Secrets Manager..."

    # Postgres connection string
    export POSTGRES_CONNECTION_STRING=$(aws secretsmanager get-secret-value \
        --region $AWS_REGION \
        --secret-id "$SECRETS_PREFIX/postgres-connection-string" \
        --query 'SecretString' --output text | jq -r '.connection_string // .')

    # Redis URL
    export REDIS_URL=$(aws secretsmanager get-secret-value \
        --region $AWS_REGION \
        --secret-id "$SECRETS_PREFIX/redis-url" \
        --query 'SecretString' --output text | jq -r '.url // .')

    # Supabase JWT secrets
    export SUPABASE_JWT_SECRETS=$(aws secretsmanager get-secret-value \
        --region $AWS_REGION \
        --secret-id "$SECRETS_PREFIX/supabase-jwt-secrets" \
        --query 'SecretString' --output text 2>/dev/null || echo "")

    # Nomad ACL token
    export NOMAD_TOKEN=$(aws secretsmanager get-secret-value \
        --region $AWS_REGION \
        --secret-id "$SECRETS_PREFIX/nomad-acl-token" \
        --query 'SecretString' --output text 2>/dev/null || echo "")

    # Consul ACL token
    export CONSUL_TOKEN=$(aws secretsmanager get-secret-value \
        --region $AWS_REGION \
        --secret-id "$SECRETS_PREFIX/consul-acl-token" \
        --query 'SecretString' --output text 2>/dev/null || echo "")

    echo "[$(date)] Secrets fetched."
}

# -----------------------------------------------------------------------------
# Configure Consul
# -----------------------------------------------------------------------------
configure_consul() {
    echo "[$(date)] Configuring Consul..."

    # Get instance metadata
    INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
    PRIVATE_IP=$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)

    mkdir -p /etc/consul.d

    cat > /etc/consul.d/consul.hcl <<EOF
datacenter = "$DATACENTER"
data_dir = "/opt/consul"
log_level = "INFO"

bind_addr = "$PRIVATE_IP"
client_addr = "0.0.0.0"

server = true
bootstrap_expect = $CONSUL_SERVER_COUNT

ui_config {
  enabled = true
}

connect {
  enabled = true
}

# Enable ACLs (will be bootstrapped separately if needed)
# acl {
#   enabled = true
#   default_policy = "deny"
#   enable_token_persistence = true
# }

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

    # Create data directory
    mkdir -p /opt/consul
    chown -R consul:consul /opt/consul

    # Enable and start Consul
    systemctl enable consul
    systemctl start consul

    echo "[$(date)] Consul configured and started."
}

# -----------------------------------------------------------------------------
# Configure Nomad
# -----------------------------------------------------------------------------
configure_nomad() {
    echo "[$(date)] Configuring Nomad..."

    INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)
    PRIVATE_IP=$(curl -s http://169.254.169.254/latest/meta-data/local-ipv4)

    mkdir -p /etc/nomad.d

    cat > /etc/nomad.d/nomad.hcl <<EOF
datacenter = "$DATACENTER"
data_dir = "/opt/nomad"
log_level = "INFO"

bind_addr = "0.0.0.0"

name = "$INSTANCE_ID"

server {
  enabled = true
  bootstrap_expect = $NOMAD_SERVER_COUNT

  # Server join configuration
  server_join {
    retry_join = ["provider=aws tag_key=nomad-server tag_value=$PREFIX-$ENVIRONMENT"]
  }
}

client {
  enabled = true

  # Node metadata
  meta {
    "node_type" = "control-plane"
  }

  # Host volumes for persistent data
  host_volume "docker-sock" {
    path = "/var/run/docker.sock"
    read_only = false
  }
}

# Consul integration
consul {
  address = "127.0.0.1:8500"
  # token = "$CONSUL_TOKEN"
}

# ACL configuration (bootstrap separately)
# acl {
#   enabled = true
# }

# Telemetry
telemetry {
  collection_interval = "1s"
  disable_hostname = true
  prometheus_metrics = true
  publish_allocation_metrics = true
  publish_node_metrics = true
}

# Plugin configuration for Docker
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
EOF

    # Create data directory
    mkdir -p /opt/nomad
    chown -R nomad:nomad /opt/nomad 2>/dev/null || true

    # Enable and start Nomad
    systemctl enable nomad
    systemctl start nomad

    echo "[$(date)] Nomad configured and started."
}

# -----------------------------------------------------------------------------
# Configure ECR Authentication
# -----------------------------------------------------------------------------
configure_ecr() {
    echo "[$(date)] Configuring ECR authentication..."

    # Write a self-contained script that bakes in env vars (cron has no env)
    cat > /usr/local/bin/ecr-login.sh <<ECREOF
#!/bin/bash
set -e
aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "$ECR_REGISTRY"
ECREOF
    chmod +x /usr/local/bin/ecr-login.sh

    # Run initial login
    /usr/local/bin/ecr-login.sh

    # Refresh ECR credentials every 6 hours (token expires every 12h)
    echo "0 */6 * * * root /usr/local/bin/ecr-login.sh >> /var/log/ecr-login.log 2>&1" > /etc/cron.d/ecr-refresh

    echo "[$(date)] ECR authentication configured."
}

# -----------------------------------------------------------------------------
# Install CloudWatch Agent
# -----------------------------------------------------------------------------
install_cloudwatch_agent() {
    echo "[$(date)] Installing CloudWatch agent..."

    wget -q https://s3.amazonaws.com/amazoncloudwatch-agent/ubuntu/amd64/latest/amazon-cloudwatch-agent.deb
    dpkg -i amazon-cloudwatch-agent.deb
    rm amazon-cloudwatch-agent.deb

    # Configure CloudWatch agent
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
        "resources": ["/"]
      }
    }
  }
}
EOF

    # Start CloudWatch agent
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
        Key=nomad-server,Value=$PREFIX-$ENVIRONMENT \
        Key=e2b-role,Value=control-plane

    echo "[$(date)] Instance tagged."
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
main() {
    echo "[$(date)] =========================================="
    echo "[$(date)] E2B Control Plane Bootstrap Starting"
    echo "[$(date)] Environment: $ENVIRONMENT"
    echo "[$(date)] Region: $AWS_REGION"
    echo "[$(date)] =========================================="

    install_dependencies
    install_hashicorp_tools
    fetch_secrets
    tag_instance
    configure_consul
    configure_nomad
    configure_ecr
    install_cloudwatch_agent

    echo "[$(date)] =========================================="
    echo "[$(date)] E2B Control Plane Bootstrap Complete!"
    echo "[$(date)] =========================================="
}

main
