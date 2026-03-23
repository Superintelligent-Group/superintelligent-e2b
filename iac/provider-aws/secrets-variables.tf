# =============================================================================
# Variables for AWS Secrets Manager
# =============================================================================

variable "create_secrets" {
  description = "Whether to create Secrets Manager secrets."
  type        = bool
  default     = true
}

variable "supabase_jwt_secrets" {
  description = "Supabase JWT secrets for authentication. Can be set later via console."
  type        = string
  default     = ""
  sensitive   = true
}

variable "nomad_acl_token" {
  description = "Nomad ACL bootstrap token. Will be generated during Nomad bootstrap."
  type        = string
  default     = ""
  sensitive   = true
}

variable "consul_acl_token" {
  description = "Consul ACL bootstrap token. Will be generated during Consul bootstrap."
  type        = string
  default     = ""
  sensitive   = true
}

variable "posthog_api_key" {
  description = "PostHog API key for analytics."
  type        = string
  default     = ""
  sensitive   = true
}

variable "launch_darkly_api_key" {
  description = "LaunchDarkly API key for feature flags."
  type        = string
  default     = ""
  sensitive   = true
}

variable "analytics_collector_host" {
  description = "Analytics collector host."
  type        = string
  default     = ""
}

variable "analytics_collector_api_token" {
  description = "Analytics collector API token."
  type        = string
  default     = ""
  sensitive   = true
}

variable "sandbox_access_token_hash_seed" {
  description = "Hash seed for sandbox access tokens. Auto-generated if not provided."
  type        = string
  default     = ""
  sensitive   = true
}
