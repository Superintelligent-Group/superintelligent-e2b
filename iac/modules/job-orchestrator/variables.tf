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
  # The smallest governed code snapshot is currently ~9.2 GiB. 4 GiB lets the
  # job schedule but guarantees Firecracker's memfd restore will fail inside
  # the cgroup. Keep a conservative default; deployments with larger guests
  # should override this alongside the template catalog.
  default     = 12288

  validation {
    condition     = var.memory_mb >= 10240
    error_message = "memory_mb must be at least 10240 MiB so a 9.2 GiB snapshot has restore headroom."
  }
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
