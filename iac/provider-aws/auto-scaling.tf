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

  # Client nodes need nested virtualization for Firecracker. Keep the scale-up
  # fleet on approved non-metal Intel 7th-gen 2xlarge shapes: they support nested
  # virtualization, cost less than the same-size 8th-gen alternatives, and avoid
  # the extreme hourly cost of bare-metal instances.
  #
  # r7i leads because sandbox hosts are memory-bound — each Firecracker microVM
  # reserves RAM — and r7i.2xlarge buys 64 GiB for ~the same spot price as
  # m8i.2xlarge's 32 GiB ($0.1875 vs $0.1854, us-east-1 2026-07-27). Ordered
  # best-value first; capacity-optimized still picks on availability, not order.
  client_spot_instance_types = [
    "r7i.2xlarge", "m7i.2xlarge", "m7i-flex.2xlarge",
    "c7i.2xlarge", "c7i-flex.2xlarge",
  ]
  api_spot_instance_types = ["t3.large", "t3a.large", "m6i.large", "m7i-flex.large"]

  # Nomad/Consul tokens for job re-evaluation after scale-up
  nomad_addr             = "https://nomad.${var.domain_name}"
  nomad_token_secret_id  = "e2b-dev/nomad-acl-token"
  consul_token_secret_id = "e2b-dev/consul-acl-token"

  tags = {
    Project   = "superintelligent-e2b"
    ManagedBy = "terraform"
  }
}

output "e2b_wake_url" {
  value       = module.auto_scaling.wake_function_url
  description = "Call this URL to wake up the E2B cluster before creating sandboxes"
}
