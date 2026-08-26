variable "node_pool" {
  type = string
}

variable "port" {
  type = number
}

variable "proxy_port" {
  type = number
}

variable "memory_mb" {
  type        = number
  description = "Nomad memory reservation for the orchestrator process and its Firecracker children. Must include guest RAM plus host/runtime headroom."
  default     = 4096
}

variable "environment" {
  type = string
}

variable "artifact_source" {
  type        = string
  description = "Full artifact URL for the orchestrator binary (e.g. gcs::https://... or s3::https://...)"
}

variable "orchestrator_checksum" {
  type        = string
  description = "Hex checksum of the orchestrator binary, used for change detection"
}

variable "job_env_vars" {
  type      = map(string)
  default   = {}
  sensitive = true
}
