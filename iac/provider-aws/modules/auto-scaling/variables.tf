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
  default     = ["c8i.2xlarge", "c7i.2xlarge", "c6i.2xlarge", "m7i.2xlarge"]
  description = "Instance types for spot fleet (client/build). Multiple types improve spot availability."
}

variable "api_spot_instance_types" {
  type        = list(string)
  default     = ["t3.medium", "t3a.medium", "t3.large"]
  description = "Instance types for spot fleet (API)"
}

variable "tags" {
  type    = map(string)
  default = {}
}
