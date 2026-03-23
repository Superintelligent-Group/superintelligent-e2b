# =============================================================================
# User Data Configuration for EC2 Launch Templates
# =============================================================================

locals {
  # Control plane user data (using templatefile to inject variables)
  control_plane_userdata = templatefile("${path.module}/scripts/control-plane-userdata.sh", {
    aws_region          = var.aws_region
    environment         = var.environment
    prefix              = var.prefix
    domain_name         = var.domain_name
    ecr_registry        = split("/", aws_ecr_repository.orchestration.repository_url)[0]
    datacenter          = var.aws_region
    nomad_server_count  = var.nomad_server_count
    consul_server_count = var.consul_server_count
  })

  # Worker user data
  worker_userdata = templatefile("${path.module}/scripts/worker-userdata.sh", {
    aws_region      = var.aws_region
    environment     = var.environment
    prefix          = var.prefix
    domain_name     = var.domain_name
    ecr_registry    = split("/", aws_ecr_repository.orchestration.repository_url)[0]
    template_bucket = local.resolved_template_bucket
    build_bucket    = local.resolved_build_bucket
    datacenter      = var.aws_region
  })

  # Use provided user data or fall back to generated scripts
  final_control_plane_userdata = var.control_plane_user_data != "" ? var.control_plane_user_data : local.control_plane_userdata
  final_worker_userdata        = var.worker_user_data != "" ? var.worker_user_data : local.worker_userdata
}

# -----------------------------------------------------------------------------
# User Data Variables
# -----------------------------------------------------------------------------

variable "nomad_server_count" {
  description = "Number of Nomad servers for bootstrap_expect."
  type        = number
  default     = 3
}

variable "consul_server_count" {
  description = "Number of Consul servers for bootstrap_expect."
  type        = number
  default     = 3
}
