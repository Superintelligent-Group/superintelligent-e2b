variable "prefix" {
  type = string
}

variable "name" {
  type = string
}

variable "aws_account_id" {
  type = string
}

variable "cluster_tag_name" {
  type = string
}

variable "cluster_tag_value" {
  type = string
}

variable "cluster_node_policy_arn" {
  type        = string
  description = "ARN of the base cluster node IAM policy"
}

variable "cluster_node_ec2_policy_json" {
  type        = string
  description = "JSON of the EC2 assume role policy document"
}

variable "setup_bucket_name" {
  type = string
}

variable "setup_files_hash" {
  type = map(string)
}

variable "security_group_ids" {
  type = list(string)
}

variable "vpc_private_subnets" {
  type = list(string)
}

variable "image_family_prefix" {
  type    = string
  default = "e2b-orch-"
}

variable "cluster_size" {
  type    = number
  default = 1
}

variable "machine_type" {
  type    = string
  default = "m7i.2xlarge"
}

variable "nested_virtualization_cpu_core_count" {
  description = "CPU core count sent with nested virtualization launch-template options."
  type        = number
  default     = 4
}

variable "nested_virtualization_threads_per_core" {
  description = "Threads per core sent with nested virtualization launch-template options."
  type        = number
  default     = 2
}

variable "node_pool_name" {
  type        = string
  description = "Nomad node pool name for client nodes"
}

variable "node_type" {
  type        = string
  description = "Explicit Nomad role for this pool; controls which system jobs may schedule here."
  default     = "worker"

  validation {
    condition     = contains(["worker", "build"], var.node_type)
    error_message = "node_type must be worker or build."
  }
}

variable "node_labels" {
  description = "Labels to assign to nodes for scheduling purposes"
  type        = list(string)
}

variable "base_hugepages_percentage" {
  description = "The percentage of memory to use for preallocated hugepages."
  type        = number
  default     = 60
}

variable "nbd_max_devices" {
  description = "Kernel NBD device capacity made available to this Firecracker node pool."
  type        = number
  default     = 4096

  validation {
    condition     = var.nbd_max_devices > 0 && var.nbd_max_devices <= 65536
    error_message = "nbd_max_devices must be between 1 and 65536."
  }
}

variable "nested_virtualization" {
  type    = bool
  default = true
}

variable "boot_disk_size_gb" {
  type        = number
  default     = 500
  description = "Root volume size in GB"
}

variable "cluster_secret_arn" {
  type        = string
  description = "Secrets Manager ARN containing the Nomad/Consul bootstrap credentials."
}

variable "aws_ecr_account_repository_domain" {
  type = string
}

variable "fc_kernels_bucket_name" {
  type = string
}

variable "fc_versions_bucket_name" {
  type = string
}

variable "fc_env_pipeline_bucket_name" {
  type = string
}

variable "fc_busybox_bucket_name" {
  type = string
}

variable "fc_env_pipeline_bucket_arn" {
  type = string
}

variable "fc_kernels_bucket_arn" {
  type = string
}

variable "fc_versions_bucket_arn" {
  type = string
}

variable "fc_busybox_bucket_arn" {
  type = string
}

variable "templates_bucket_arn" {
  type = string
}

variable "templates_build_cache_bucket_arn" {
  type = string
}

variable "custom_environments_repo_arn" {
  type = string
}

variable "scripts_path" {
  type        = string
  description = "Path to the directory containing startup scripts. Defaults to in-module scripts."
  default     = ""
}
