# =============================================================================
# Variables for AWS Nomad Jobs
# =============================================================================

# -----------------------------------------------------------------------------
# General Configuration
# -----------------------------------------------------------------------------

variable "aws_region" {
  description = "AWS region."
  type        = string
}

variable "prefix" {
  description = "Resource prefix."
  type        = string
  default     = "e2b"
}

variable "environment" {
  description = "Environment name (prod, staging, dev)."
  type        = string
}

variable "nomad_address" {
  description = "Nomad server address."
  type        = string
  default     = "http://localhost:4646"
}

variable "use_secrets_manager" {
  description = "Whether to fetch secrets from AWS Secrets Manager."
  type        = bool
  default     = true
}

variable "use_managed_redis" {
  description = "Whether to use ElastiCache Redis instead of Nomad Redis job."
  type        = bool
  default     = true
}

# -----------------------------------------------------------------------------
# Node Pools
# -----------------------------------------------------------------------------

variable "api_node_pool" {
  description = "Nomad node pool for API services."
  type        = string
  default     = "default"
}

variable "orchestrator_node_pool" {
  description = "Nomad node pool for orchestrator."
  type        = string
  default     = "workers"
}

variable "builder_node_pool" {
  description = "Nomad node pool for template manager/builder."
  type        = string
  default     = "workers"
}

# -----------------------------------------------------------------------------
# Service Ports
# -----------------------------------------------------------------------------

variable "api_port" {
  description = "API service port."
  type        = number
  default     = 80
}

variable "orchestrator_port" {
  description = "Orchestrator gRPC port."
  type        = number
  default     = 5008
}

variable "orchestrator_proxy_port" {
  description = "Orchestrator proxy port."
  type        = number
  default     = 5009
}

variable "redis_port" {
  description = "Redis port."
  type        = number
  default     = 6379
}

variable "edge_proxy_port" {
  description = "Edge/client proxy port."
  type        = number
  default     = 49982
}

variable "edge_api_port" {
  description = "Edge API port."
  type        = number
  default     = 3001
}

variable "template_manager_port" {
  description = "Template manager gRPC port."
  type        = number
  default     = 5007
}

variable "otel_collector_grpc_port" {
  description = "OpenTelemetry collector gRPC port."
  type        = number
  default     = 4317
}

variable "logs_proxy_port" {
  description = "Logs collector proxy port."
  type        = number
  default     = 3100
}

variable "loki_service_port" {
  description = "Loki service port."
  type        = number
  default     = 3100
}

# -----------------------------------------------------------------------------
# Service Counts and Resources
# -----------------------------------------------------------------------------

variable "api_count" {
  description = "Number of API instances."
  type        = number
  default     = 2
}

variable "api_memory_mb" {
  description = "API memory in MB."
  type        = number
  default     = 1024
}

variable "api_cpu_count" {
  description = "API CPU count."
  type        = number
  default     = 1
}

variable "client_proxy_count" {
  description = "Number of client proxy instances."
  type        = number
  default     = 2
}

variable "client_proxy_memory_mb" {
  description = "Client proxy memory in MB."
  type        = number
  default     = 512
}

variable "client_proxy_cpu_count" {
  description = "Client proxy CPU count."
  type        = number
  default     = 1
}

variable "client_proxy_update_max_parallel" {
  description = "Max parallel updates for client proxy."
  type        = number
  default     = 1
}

variable "template_manager_count" {
  description = "Number of template manager instances."
  type        = number
  default     = 1
}

# -----------------------------------------------------------------------------
# Docker Images
# -----------------------------------------------------------------------------

variable "api_docker_image" {
  description = "Docker image for API service."
  type        = string
}

variable "db_migrator_docker_image" {
  description = "Docker image for database migrator."
  type        = string
}

variable "client_proxy_docker_image" {
  description = "Docker image for client proxy."
  type        = string
}

variable "ecr_repository_url" {
  description = "ECR repository URL for custom environments."
  type        = string
}

# -----------------------------------------------------------------------------
# Artifact Checksums
# -----------------------------------------------------------------------------

variable "orchestrator_checksum" {
  description = "MD5 checksum of orchestrator binary."
  type        = string
  default     = ""
}

variable "template_manager_checksum" {
  description = "MD5 checksum of template manager binary."
  type        = string
  default     = ""
}

# -----------------------------------------------------------------------------
# S3 Buckets
# -----------------------------------------------------------------------------

variable "template_bucket_name" {
  description = "S3 bucket for templates."
  type        = string
}

variable "build_bucket_name" {
  description = "S3 bucket for build artifacts."
  type        = string
}

variable "build_cache_bucket_name" {
  description = "S3 bucket for build cache."
  type        = string
  default     = ""
}

# -----------------------------------------------------------------------------
# Configuration Settings
# -----------------------------------------------------------------------------

variable "otel_tracing_print" {
  description = "Enable OTEL tracing print."
  type        = string
  default     = "false"
}

variable "envd_timeout" {
  description = "Envd timeout."
  type        = string
  default     = "60s"
}

variable "allow_sandbox_internet" {
  description = "Allow sandbox internet access."
  type        = string
  default     = "true"
}

variable "shared_chunk_cache_path" {
  description = "Shared chunk cache path."
  type        = string
  default     = ""
}

variable "dockerhub_remote_repository_url" {
  description = "DockerHub remote repository URL."
  type        = string
  default     = ""
}

# -----------------------------------------------------------------------------
# Secrets (only used when use_secrets_manager = false)
# -----------------------------------------------------------------------------

variable "postgres_connection_string" {
  description = "PostgreSQL connection string."
  type        = string
  default     = ""
  sensitive   = true
}

variable "redis_url" {
  description = "Redis URL."
  type        = string
  default     = ""
  sensitive   = true
}

variable "supabase_jwt_secrets" {
  description = "Supabase JWT secrets."
  type        = string
  default     = ""
  sensitive   = true
}

variable "admin_token" {
  description = "API admin token."
  type        = string
  default     = ""
  sensitive   = true
}

variable "nomad_acl_token" {
  description = "Nomad ACL token."
  type        = string
  default     = ""
  sensitive   = true
}

variable "consul_acl_token" {
  description = "Consul ACL token."
  type        = string
  default     = ""
  sensitive   = true
}

variable "posthog_api_key" {
  description = "PostHog API key."
  type        = string
  default     = ""
  sensitive   = true
}

variable "launch_darkly_api_key" {
  description = "LaunchDarkly API key."
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

variable "clickhouse_connection_string" {
  description = "ClickHouse connection string."
  type        = string
  default     = ""
  sensitive   = true
}

variable "sandbox_access_token_hash_seed" {
  description = "Sandbox access token hash seed."
  type        = string
  default     = ""
  sensitive   = true
}

variable "edge_api_secret" {
  description = "Edge API secret."
  type        = string
  default     = ""
  sensitive   = true
}
