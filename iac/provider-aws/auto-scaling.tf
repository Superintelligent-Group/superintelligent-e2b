# =============================================================================
# Auto-Scaling: Scale-to-zero + spot instances for E2B cluster
# =============================================================================
# This is our custom addition on top of the upstream E2B infra.
# Provides automatic wake-up (via Lambda URL) and idle shutdown.
# =============================================================================

module "auto_scaling" {
  source = "./modules/auto-scaling"

  prefix = var.prefix

  control_server_asg_name = "${var.prefix}control-server"
  api_asg_name            = "${var.prefix}api"
  client_asg_name         = "${var.prefix}orch-client"
  build_asg_name          = "${var.prefix}orch-build"

  idle_timeout_minutes = 30

  # Multiple instance types improve spot availability
  client_spot_instance_types = ["c8i.2xlarge", "c7i.2xlarge", "c6i.2xlarge", "m7i.2xlarge"]
  api_spot_instance_types    = ["t3.large", "t3a.large", "m6i.large"]

  tags = {
    Project   = "superintelligent-e2b"
    ManagedBy = "terraform"
  }
}

output "e2b_wake_url" {
  value       = module.auto_scaling.wake_function_url
  description = "Call this URL to wake up the E2B cluster before creating sandboxes"
}
