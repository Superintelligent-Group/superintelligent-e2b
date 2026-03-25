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

variable "allow_sandbox_internet" {
  type    = bool
  default = true
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

variable "additional_traefik_arguments" {
  type    = list(string)
  default = []
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
