# COST-00 P3 (SUP-619): repointed to CommonQuant (014155356804).
# CQ-issued *.e2b certificate; buckets are the CQ ones created by the
# superintelligent.group e2b module. SIG reference: dev.tfvars.sig-backup.
# =============================================================================
# E2B Self-Hosted — Dev Environment Configuration
# =============================================================================

aws_region = "us-east-1"
# SUP-676: was "e2b" (no trailing dash) -- didn't match what's actually
# deployed ("e2b-vpc", "e2b-api-node", etc). Confirmed via a full targeted
# plan: with the dash restored, live infra matches config exactly (no
# phantom replacements of IAM roles/security groups/ALB target groups/the
# cluster secret). Drift predates this repo's git history (both this file's
# first commit and dev.sig.tfvars.reference already lacked the dash) --
# whatever created the live resources used a different value that was never
# reconciled back into tracked config.
prefix                = "e2b-"
environment           = "dev"
launch_darkly_enabled = false
# Explicitly override the image's legacy baked key while the dev environment
# uses the offline feature-flag store.
api_env_vars = {
  LAUNCH_DARKLY_API_KEY = ""
}
# Required by module.init bucket names; reviewed instead of synthesized through
# a higher-precedence TF_VAR_bucket_prefix environment value.
bucket_prefix = "e2b-014155356804-"

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
# metal) is supported on approved Intel 7th/8th-gen C7i/M7i/R7i and
# C8i/M8i/R8i families plus the documented flex variants. Not Graviton. This
# is an explicit capability requirement, not a price preference: the launch
# request must include the full CPU option tuple, or EC2 omits the setting.
#
#   8 vCPU tier      on-demand   spot     GiB
#   c7i-flex.2xl      reviewed low-cost candidate
#   c7i.2xl           reviewed low-cost candidate
#   m7i.2xl           reviewed low-cost candidate
#   r7i.2xl           reviewed memory-density candidate
#
# Sandbox hosts are memory-bound (each Firecracker microVM reserves RAM), so
# r7i is the memory-oriented candidate for sandbox density and belongs in the
# override list. Launch-template defaults stay NON-flex: flex variants trade sustained CPU
# for price, which is fine when capacity-optimized picks them opportunistically
# and bad as the guaranteed fallback shape.
# -----------------------------------------------------------------------------
client_server_machine_type = "m7i.2xlarge" # supported non-metal nested virtualization shape
api_server_machine_type    = "t3.large"    # was t3.xlarge default; matches api_spot_instance_types
build_server_machine_type  = "m7i.2xlarge" # supported non-metal nested virtualization shape
