variable "prefix" {
  type = string
}

variable "control_server_asg_name" {
  type        = string
  description = "ASG name for Nomad control server"
}

variable "api_asg_name" {
  type        = string
  description = "ASG name for API node pool"
}

variable "client_asg_name" {
  type        = string
  description = "ASG name for client (Firecracker) node pool"
}

variable "build_asg_name" {
  type        = string
  description = "ASG name for build node pool"
}

variable "idle_timeout_minutes" {
  type        = number
  default     = 30
  description = "Minutes of no activity before scaling to zero"
}

variable "client_spot_instance_types" {
  type        = list(string)
  default     = ["c8i.2xlarge", "m8i.2xlarge", "c8i-flex.2xlarge", "m8i-flex.2xlarge"]
  description = "Instance types for spot fleet (client/build). Multiple types improve spot availability."
}

variable "api_spot_instance_types" {
  type        = list(string)
  default     = ["t3.medium", "t3a.medium", "t3.large"]
  description = "Instance types for spot fleet (API)"
}

variable "nomad_addr" {
  type        = string
  default     = ""
  description = "Nomad API address (e.g. https://nomad.e2b.superintelligent.group)"
}

variable "nomad_token_secret_id" {
  type        = string
  default     = ""
  description = "Secrets Manager ID for Nomad ACL token"
}

variable "consul_token_secret_id" {
  type        = string
  default     = ""
  description = "Secrets Manager ID for Consul ACL token"
}

variable "tags" {
  type    = map(string)
  default = {}
}
