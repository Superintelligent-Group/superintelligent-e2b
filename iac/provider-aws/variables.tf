variable "domain_name" {
  type = string
}

variable "allow_force_destroy" {
  default = false
}

variable "prefix" {
  type        = string
  description = "Name prefix for all resources"
}

variable "bucket_prefix" {
  type = string
}

variable "environment" {
  type = string
}

variable "redis_managed" {
  type    = bool
  default = false
}

variable "redis_instance_type" {
  type    = string
  default = "cache.t2.small"
}

variable "redis_replica_size" {
  type    = number
  default = 2
}

variable "api_cluster_size" {
  type    = number
  default = 1
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

variable "client_proxy_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}

variable "orchestrator_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}

variable "template_manager_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}

variable "s3_use_path_style" {
  type        = bool
  default     = false
  description = "When true, use path-style S3 addressing (https://host/bucket/key). When false (default), use virtual-host-style (https://bucket.host/key). Set to true for S3-compatible backends (MinIO, Ceph, etc.) that don't support virtual-host addressing."
}

variable "api_server_machine_type" {
  type    = string
  default = "t3.xlarge"
}

variable "api_image_family_prefix" {
  type    = string
  default = ""
}

variable "ingress_count" {
  type    = number
  default = 1
}

variable "client_proxy_count" {
  type    = number
  default = 1
}

# --- Resource overrides (right-size Nomad jobs for dev clusters) ---
# --- Resource overrides ---
# Dev defaults: all jobs fit on t3.large (2 vCPU = 2048 CPU shares)
# Budget: redis(500) + ingress(300) + otel(200) + otel-nomad(100) + api(500) + client-proxy(300) = 1900
variable "redis_cpu" {
  type    = number
  default = 500
}

variable "redis_memory_mb" {
  type    = number
  default = 1024
}

variable "ingress_cpu_count" {
  type    = number
  default = 0.3
}

variable "ingress_memory_mb" {
  type    = number
  default = 256
}

variable "client_proxy_cpu_count" {
  type    = number
  default = 0.3
}

variable "client_proxy_memory_mb" {
  type    = number
  default = 256
}

variable "otel_cpu_count" {
  type    = number
  default = 0.2
}

variable "otel_memory_mb" {
  type    = number
  default = 256
}

variable "loki_enabled" {
  type    = bool
  default = false
}

variable "loki_cpu_count" {
  type    = number
  default = 0.3
}

variable "loki_memory_mb" {
  type    = number
  default = 256
}

variable "clickhouse_cluster_size" {
  type    = number
  default = 1
}

variable "clickhouse_server_machine_type" {
  type    = string
  default = "t3.xlarge"
}

variable "clickhouse_image_family_prefix" {
  type    = string
  default = ""
}

variable "client_cluster_size" {
  type    = number
  default = 1
}

variable "client_server_machine_type" {
  type    = string
  default = "m8i.4xlarge"
}

variable "client_server_nested_virtualization" {
  type    = bool
  default = true
}

variable "client_node_labels" {
  description = "Labels to assign to client nodes for scheduling purposes"
  type        = list(string)
  default     = []
}

variable "client_image_family_prefix" {
  type    = string
  default = ""
}

variable "control_server_machine_type" {
  type    = string
  default = "t3.medium"
}

variable "control_server_image_family_prefix" {
  type    = string
  default = ""
}

variable "orchestrator_port" {
  type    = number
  default = 5008
}

variable "orchestrator_proxy_port" {
  type    = number
  default = 5007
}

variable "allow_sandbox_internal_cidrs" {
  type        = string
  description = "Comma-separated CIDRs to allow through the sandbox firewall deny list (e.g. 10.0.0.1/32,10.0.0.2/32)"
  default     = ""
}

variable "envd_timeout" {
  type    = string
  default = "40s"
}

variable "build_cluster_size" {
  type    = number
  default = 1
}

variable "build_server_machine_type" {
  type    = string
  default = "m8i.2xlarge"
}

variable "build_server_nested_virtualization" {
  type    = bool
  default = true
}

variable "build_node_labels" {
  description = "Labels to assign to build nodes for scheduling purposes"
  type        = list(string)
  default     = []
}

variable "control_server_cluster_size" {
  type    = number
  default = 3
}

variable "traefik_config_files" {
  type        = map(string)
  description = "Map of filename => content for additional Traefik dynamic configuration files"
  default     = {}
}

variable "db_max_open_connections" {
  type    = number
  default = 40
}

variable "db_min_idle_connections" {
  type    = number
  default = 5
}

variable "auth_db_max_open_connections" {
  type    = number
  default = 20
}

variable "auth_db_min_idle_connections" {
  type    = number
  default = 5
}

# COST-00 P3 (SUP-619): the Route53 zone this stack manages records in, when it
# is NOT the registrable root of var.domain_name. On CommonQuant the apex zone
# superintelligent.group remains SIG-owned and only e2b.superintelligent.group
# is delegated here, so this points at the delegated subdomain zone. Empty
# preserves upstream behaviour (derive the root from domain_name).
variable "hosted_zone_name" {
  description = "Explicit Route53 hosted zone name to use instead of the derived registrable root."
  type        = string
  default     = ""
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
