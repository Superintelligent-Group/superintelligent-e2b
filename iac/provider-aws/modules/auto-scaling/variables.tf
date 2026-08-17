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
  default     = ["r7i.2xlarge", "m7i.2xlarge", "c7i.2xlarge"]
  description = "Approved non-metal Intel 7th-gen 2xlarge instance types for the client spot fleet."

  validation {
    condition = length(var.client_spot_instance_types) > 0 && alltrue([
      for instance_type in var.client_spot_instance_types :
      contains([
        "r7i.2xlarge",
        "m7i.2xlarge",
        "m7i-flex.2xlarge",
        "c7i.2xlarge",
        "c7i-flex.2xlarge",
      ], instance_type)
    ])
    error_message = "Client spot types must contain at least one approved non-metal Intel 7th/8th-gen 2xlarge instance (C7i/M7i/R7i or C8i/M8i/R8i)."
  }
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
