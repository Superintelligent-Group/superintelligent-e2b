# Core
variable "domain_name" {
  type = string
}

variable "environment" {
  type = string
}

variable "aws_region" {
  type = string
}

# Auth
variable "nomad_acl_token" {
  type      = string
  sensitive = true
}

variable "consul_acl_token" {
  type      = string
  sensitive = true
}

# Node pools
variable "api_node_pool" {
  type = string
}

variable "clickhouse_node_pool" {
  type = string
}

variable "clickhouse_jobs_prefix" {
  type = string
}

# Cluster sizes -- api_cluster_size tracks node-pool capacity (used for
# update_stanza/prevent_colocation decisions: is there enough spare capacity
# to roll a canary or spread jobs across hosts). It is NOT the api job's own
# instance count -- see api_count below, matching the independent
# ingress_count/client_proxy_count pattern (a job with its own node-pool
# capacity var and instance count coupled together sits permanently "dead"
# whenever that pool is scaled to zero for cost savings, since scaling the
# ASG back up doesn't retroactively bump a Terraform-managed job count).
variable "api_cluster_size" {
  type = number
}

variable "api_count" {
  type    = number
  default = 1
}

# Ingress
variable "ingress_port" {
  type        = number
  description = "External traffic port number"
}

variable "ingress_internal_port" {
  type        = number
  description = "Internal traffic port number"
}

variable "ingress_count" {
  type = number
}

# Client Proxy
variable "client_proxy_count" {
  type    = number
  default = 1
}

variable "client_proxy_repository_name" {
  type = string
}

variable "client_proxy_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}

# Redis
variable "redis_managed" {
  type = bool
}

variable "redis_port" {
  type = number
}

# ClickHouse
variable "clickhouse_cluster_size" {
  type = number
}

variable "clickhouse_username" {
  type    = string
  default = "e2b"
}

variable "clickhouse_password" {
  type      = string
  sensitive = true
}

variable "clickhouse_server_secret" {
  type      = string
  sensitive = true
}

variable "clickhouse_port" {
  type    = number
  default = 9000
}

variable "clickhouse_cpu_count" {
  type    = number
  default = 4
}

variable "clickhouse_memory_mb" {
  type    = number
  default = 8192
}

variable "clickhouse_database" {
  type    = string
  default = "default"
}

variable "clickhouse_metrics_port" {
  type    = number
  default = 9363
}

variable "clickhouse_backups_bucket_name" {
  type = string
}

variable "clickhouse_migrator_repository_name" {
  type = string
}

# Grafana / Observability
variable "grafana_otel_collector_token" {
  type      = string
  sensitive = true
}

variable "grafana_otlp_url" {
  type      = string
  sensitive = true
}

variable "grafana_username" {
  type      = string
  sensitive = true
}

# API
variable "api_port" {
  type    = number
  default = 80
}

variable "api_internal_grpc_port" {
  type    = number
  default = 5009
}

variable "api_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}

variable "api_db_migrator_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}

variable "api_memory_mb" {
  type    = number
  default = 512
}

variable "api_cpu_count" {
  type    = number
  default = 0.5
}

variable "api_repository_name" {
  type = string
}

variable "db_migrator_repository_name" {
  type = string
}

# Orchestrator
variable "orchestrator_node_pool" {
  type = string
}

variable "orchestrator_port" {
  type    = number
  default = 5008
}

variable "orchestrator_proxy_port" {
  type    = number
  default = 5007
}

variable "orchestrator_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}

variable "fc_env_pipeline_bucket_name" {
  type = string
}

variable "template_bucket_name" {
  type = string
}

variable "build_cache_bucket_name" {
  type    = string
  default = ""
}

variable "custom_environments_repository_name" {
  type = string
}

# Template Manager
variable "build_node_pool" {
  type = string
}

variable "template_manager_port" {
  type    = number
  default = 5008
}

variable "template_manager_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}

variable "build_cluster_size" {
  type    = number
  default = 1
}

# Loki
variable "loki_bucket_name" {
  type = string
}

variable "loki_port" {
  type    = number
  default = 3100
}

variable "logs_health_proxy_port" {
  type    = number
  default = 44313
}

# Telemetry
variable "otel_collector_grpc_port" {
  type    = number
  default = 4317
}

variable "logs_proxy_port" {
  type    = number
  default = 30006
}

# --- Resource overrides (right-size for dev / small clusters) ---
variable "ingress_cpu_count" {
  type    = number
  default = 1
}

variable "ingress_memory_mb" {
  type    = number
  default = 512
}

variable "client_proxy_cpu_count" {
  type    = number
  default = 1
}

variable "client_proxy_memory_mb" {
  type    = number
  default = 512
}

variable "redis_cpu" {
  type    = number
  default = 1000
}

variable "redis_memory_mb" {
  type    = number
  default = 2048
}

variable "otel_cpu_count" {
  type    = number
  default = 1
}

variable "otel_memory_mb" {
  type    = number
  default = 512
}

variable "loki_enabled" {
  description = "Whether to deploy Loki for log aggregation (can skip in dev)"
  type        = bool
  default     = true
}

variable "loki_cpu_count" {
  type    = number
  default = 1
}

variable "loki_memory_mb" {
  type    = number
  default = 512
}

variable "enable_otel_router_logs" {
  type        = bool
  default     = false
  description = "Enable teeing non-internal customer logs from Vector to otel-router."
}

variable "otel_router_http_port" {
  type        = number
  default     = 4321
  description = "Local otel-router Vector-compatible logs port used by Vector when otel-router log teeing is enabled."
}

variable "enable_otel_router_metrics" {
  type        = bool
  default     = false
  description = "Enable teeing external customer metrics from otel-collector to otel-router."
}

variable "otel_router_grpc_port" {
  type        = number
  default     = 4320
  description = "Local otel-router OTLP gRPC port used by otel-collector when otel-router metric teeing is enabled."
}

# Feature flags
variable "launch_darkly_api_key" {
  type      = string
  default   = ""
  sensitive = true
}

variable "traefik_config_files" {
  type = map(string)
}
