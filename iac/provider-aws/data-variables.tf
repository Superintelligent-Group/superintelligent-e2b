# =============================================================================
# Variables for Data Layer (RDS, ElastiCache)
# =============================================================================

# -----------------------------------------------------------------------------
# RDS PostgreSQL Variables
# -----------------------------------------------------------------------------

variable "create_rds" {
  description = "Whether to create RDS PostgreSQL instance. Set to false if using external database."
  type        = bool
  default     = true
}

variable "rds_postgres_version" {
  description = "PostgreSQL engine version."
  type        = string
  default     = "15.4"
}

variable "rds_instance_class" {
  description = "RDS instance class."
  type        = string
  default     = "db.t3.medium"
}

variable "rds_allocated_storage" {
  description = "Initial allocated storage in GB."
  type        = number
  default     = 20
}

variable "rds_max_allocated_storage" {
  description = "Maximum storage for autoscaling in GB."
  type        = number
  default     = 100
}

variable "rds_database_name" {
  description = "Name of the default database."
  type        = string
  default     = "e2b"
}

variable "rds_username" {
  description = "Master username for RDS."
  type        = string
  default     = "e2b_admin"
}

variable "rds_multi_az" {
  description = "Enable Multi-AZ deployment for high availability."
  type        = bool
  default     = false
}

variable "rds_backup_retention_days" {
  description = "Number of days to retain automated backups."
  type        = number
  default     = 7
}

variable "rds_performance_insights" {
  description = "Enable Performance Insights."
  type        = bool
  default     = true
}

variable "rds_kms_key_id" {
  description = "KMS key ID for RDS encryption. Leave blank for AWS managed key."
  type        = string
  default     = ""
}

variable "external_postgres_connection_string" {
  description = "Connection string for external PostgreSQL if not using RDS."
  type        = string
  default     = ""
  sensitive   = true
}

# -----------------------------------------------------------------------------
# ElastiCache Redis Variables
# -----------------------------------------------------------------------------

variable "create_elasticache" {
  description = "Whether to create ElastiCache Redis cluster. Set to false if using external Redis."
  type        = bool
  default     = true
}

variable "redis_engine_version" {
  description = "Redis engine version."
  type        = string
  default     = "7.0"
}

variable "redis_node_type" {
  description = "ElastiCache node type."
  type        = string
  default     = "cache.t3.medium"
}

variable "redis_num_cache_clusters" {
  description = "Number of cache clusters (nodes) for the replication group."
  type        = number
  default     = 2
}

variable "redis_transit_encryption" {
  description = "Enable in-transit encryption (TLS)."
  type        = bool
  default     = true
}

variable "redis_auth_enabled" {
  description = "Enable Redis AUTH token."
  type        = bool
  default     = true
}

variable "redis_snapshot_retention_days" {
  description = "Number of days to retain Redis snapshots."
  type        = number
  default     = 7
}

variable "external_redis_url" {
  description = "Connection URL for external Redis if not using ElastiCache."
  type        = string
  default     = ""
  sensitive   = true
}

# -----------------------------------------------------------------------------
# ClickHouse Variables (External - not provisioned by Terraform)
# -----------------------------------------------------------------------------

variable "clickhouse_host" {
  description = "ClickHouse host address. Must be provisioned externally."
  type        = string
  default     = ""
}

variable "clickhouse_port" {
  description = "ClickHouse port."
  type        = number
  default     = 9000
}

variable "clickhouse_database" {
  description = "ClickHouse database name."
  type        = string
  default     = "e2b"
}

variable "clickhouse_username" {
  description = "ClickHouse username."
  type        = string
  default     = "default"
}

variable "clickhouse_password" {
  description = "ClickHouse password."
  type        = string
  default     = ""
  sensitive   = true
}
