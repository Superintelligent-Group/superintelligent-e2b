# COST-00 P3 (SUP-619): repointed to CommonQuant (014155356804).
# CQ-issued *.e2b certificate; buckets are the CQ ones created by the
# superintelligent.group e2b module. SIG reference: dev.tfvars.sig-backup.
# =============================================================================
# E2B Self-Hosted — Dev Environment Configuration
# =============================================================================

aws_region  = "us-east-1"
prefix      = "e2b"
environment = "dev"

# -----------------------------------------------------------------------------
# Networking
# -----------------------------------------------------------------------------
vpc_cidr             = "10.10.0.0/16"
availability_zones   = ["us-east-1a", "us-east-1b"]
public_subnet_cidrs  = ["10.10.1.0/24", "10.10.2.0/24"]
private_subnet_cidrs = ["10.10.10.0/24", "10.10.11.0/24"]
single_nat_gateway   = true # Save ~$66/month in dev

# -----------------------------------------------------------------------------
# Domain & TLS
# -----------------------------------------------------------------------------
domain_name = "e2b.superintelligent.group"
# Delegated subdomain zone in CQ; apex stays SIG-owned (COST-00 P3).
hosted_zone_name    = "e2b.superintelligent.group"
acm_certificate_arn = "arn:aws:acm:us-east-1:014155356804:certificate/152016f3-f662-42b6-8a99-763d2d991dee"

# DNS — using Cloudflare (leave Route53 disabled)
create_route53_record = false

# -----------------------------------------------------------------------------
# S3 Buckets (already exist from prior setup)
# -----------------------------------------------------------------------------
template_bucket_name = "e2b-dev-templates-014155356804"
snapshot_bucket_name = "e2b-dev-snapshots-014155356804"
build_bucket_name    = "e2b-dev-builds-014155356804"
log_bucket_name      = "e2b-dev-logs-014155356804"

# -----------------------------------------------------------------------------
# Control Plane (Nomad server + API)
# -----------------------------------------------------------------------------
control_plane_ami_id           = "ami-04eaa218f1349d88b" # Ubuntu 24.04 (2026-03-21)
control_plane_instance_type    = "c6a.large"             # 2 vCPU, 4 GB — adequate for dev
control_plane_min_size         = 1
control_plane_desired_capacity = 1 # Single node for dev (saves ~$44/month)
control_plane_max_size         = 3

# Nomad/Consul bootstrap — single-node for dev
nomad_server_count  = 1
consul_server_count = 1

# -----------------------------------------------------------------------------
# Workers (Firecracker sandbox hosts)
# -----------------------------------------------------------------------------
worker_ami_id             = "ami-04eaa218f1349d88b" # Ubuntu 24.04 (2026-03-21)
worker_instance_type      = "c8i.2xlarge"           # 8 vCPU, 16 GB — ~50 concurrent sandboxes
worker_enable_nested_virt = true                    # Required for Firecracker KVM on virtual instances
worker_min_size           = 0                       # Scale-to-zero when idle
worker_desired_capacity   = 0                       # Start at zero, scale up on demand
worker_max_size           = 3
worker_root_volume_size   = 200 # GB — templates, snapshots, Firecracker images

# -----------------------------------------------------------------------------
# Storage
# -----------------------------------------------------------------------------
root_volume_size         = 50 # GB for control plane
enable_bucket_versioning = true

# -----------------------------------------------------------------------------
# Data Layer
# -----------------------------------------------------------------------------
create_rds               = true
rds_instance_class       = "db.t4g.micro" # Smallest for dev ($12/month)
rds_allocated_storage    = 20
rds_multi_az             = false
rds_performance_insights = false # Save cost in dev

create_elasticache       = true
redis_node_type          = "cache.t4g.micro" # Smallest for dev ($12/month)
redis_num_cache_clusters = 1                 # Single node, no replication in dev
redis_transit_encryption = true
redis_auth_enabled       = true

# -----------------------------------------------------------------------------
# Secrets (will be set post-deploy via AWS console/CLI)
# -----------------------------------------------------------------------------
create_secrets = true

# -----------------------------------------------------------------------------
# Tags
# -----------------------------------------------------------------------------
tags = {
  Project     = "superintelligent-e2b"
  Environment = "dev"
  ManagedBy   = "terraform"
}

# -----------------------------------------------------------------------------
# COST-00 P3 (SUP-619): nodepool sizing matched to SIG's ACTUAL dev posture, not
# the upstream production defaults. Upstream ships control_server=3 and api /
# client / build / clickhouse = 1 each, on m8i/c8i nested-virt instances — that
# is roughly $1.5-3k/mo and would defeat the entire migration. SIG's live dev
# cluster runs ONE control server with every other pool at zero, scaling up on
# demand via the wake path. Match that.
# -----------------------------------------------------------------------------
control_server_cluster_size = 1
api_cluster_size            = 0
client_cluster_size         = 0
build_cluster_size          = 0
clickhouse_cluster_size     = 0

# -----------------------------------------------------------------------------
# Machine types. The block above zeroes the pool COUNTS but never overrode the
# pool SIZES, so the launch templates still carried the upstream defaults —
# and those are larger than the sizes our own wake path actually asks for:
#
#   pool     upstream LT default   spot list in auto-scaling.tf   on-demand/hr
#   client   m8i.4xlarge           *.2xlarge                      $0.8467
#   api      t3.xlarge             t3.large / t3a.large / m6i.large  $0.1664
#
# cluster_scaler.py converts to 100% spot at the smaller sizes on wake, so the
# happy path was already cheap. The hole is the FALLBACK: when spot capacity is
# unavailable the Lambda catches and calls _scale_asg(), which uses the launch
# template default — silently landing on m8i.4xlarge on-demand at $0.847/hr,
# 4.3x the $0.199/hr the spot path intended. The most expensive configuration
# available was the one reached by failure.
#
# Align the launch-template defaults with the sizes the spot lists already
# declare, so degraded == smaller, never larger. Build stays at 2xlarge: template
# builds are CPU-bound and it is scale-to-zero, so undersizing it buys little and
# risks failed builds.
#
# Family choice. Nested virtualization (Firecracker/KVM inside EC2, no bare
# metal) is supported on Intel 7th AND 8th gen — C7i/M7i/R7i and C8i/M8i/R8i,
# plus flex variants. Not Graviton. Our previous comment said "only c8i/m8i",
# which was wrong and cost us: measured us-east-1 2026-07-27, 8i appears exactly
# ONCE on the spot Pareto frontier and NEVER on the on-demand frontier. Every
# other non-dominated point is 7i. We were defaulting to the dominated generation.
#
#   8 vCPU tier      on-demand   spot     GiB
#   c7i-flex.2xl      0.3392    0.1424    16
#   c7i.2xl           0.3570    0.1472    16
#   m7i.2xl           0.4032    0.1742    32
#   m8i.2xl (old)     0.4234    0.1854    32
#   r7i.2xl           0.5292    0.1875    64   <- 2x the RAM of m7i for +0.013 spot
#
# Sandbox hosts are memory-bound (each Firecracker microVM reserves RAM), so
# r7i is the best value per sandbox on the spot path and belongs in the override
# list. Launch-template defaults stay NON-flex: flex variants trade sustained CPU
# for price, which is fine when capacity-optimized picks them opportunistically
# and bad as the guaranteed fallback shape.
# -----------------------------------------------------------------------------
client_server_machine_type = "m7i.2xlarge" # was m8i.4xlarge default: $0.8467/h -> $0.4032/h (-52%)
api_server_machine_type    = "t3.large"    # was t3.xlarge default; matches api_spot_instance_types
build_server_machine_type  = "m7i.2xlarge" # was m8i.2xlarge default: $0.4234/h -> $0.4032/h
