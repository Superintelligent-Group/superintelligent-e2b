variable "aws_region" {
  type = string
}

variable "aws_profile" {
  type = string
}

variable "source_ami_filter_name" {
  type    = string
  default = "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"
}

variable "prefix" {
  type = string
}

variable "consul_version" {
  type = string
  # Keep the client image on the same line as the CQ control plane. Leaving
  # this stale forces worker userdata to reinstall Consul during every cold
  # scale-up and defeats the immutable-image contract.
  default = "2.0.3"
}

variable "nomad_version" {
  type = string
  # Keep the immutable node image on the same major/minor line as the live
  # Nomad servers.  A client from an older line can boot successfully while
  # silently failing to register or interpret newer jobspec fields, leaving
  # paid capacity invisible to the scheduler.  Override this explicitly when
  # building a deliberately different compatibility image.
  default = "2.0.5"
}

# Keep in sync with `clickhouse_version` in iac/modules/job-clickhouse/variables.tf
variable "clickhouse_client_version" {
  type    = string
  default = "25.4.5.24"
}

variable "cni_plugin_version" {
  type    = string
  default = "v1.6.2"
}

variable "firecracker_host_version" {
  type        = string
  description = "Exact Firecracker and jailer release baked into the worker host image."
  default     = "v1.5.0"
}

variable "firecracker_runtime_version" {
  type        = string
  description = "Exact E2B Firecracker build baked into the versioned runtime path selected by template metadata."
  default     = "v1.14.1_431f1fc"
}

variable "firecracker_runtime_sha256" {
  type        = string
  description = "SHA-256 of the pinned amd64 E2B Firecracker runtime binary."
  default     = "d81fd733be7e027406b4d5241442c447a2b5878b06dfa63dc236e68f3536d689"
}

variable "cloudwatch_agent_version" {
  type        = string
  description = "Exact Amazon CloudWatch agent package version baked into the worker host image."
  default     = "1.300072.0b1766"
}

variable "cloudwatch_agent_sha256" {
  type        = string
  description = "SHA-256 of the pinned CloudWatch agent Debian package."
  default     = "05baeadca96c4bb8e43906ed09cf0bebd0f321ff6d41987bdc46ce681de0978d"
}

variable "base_instance_type" {
  type    = string
  default = "t3.large"
}

variable "vpc_id" {
  type    = string
  default = ""
}

variable "subnet_id" {
  type    = string
  default = ""
}
