variable "prefix" {
  type = string
}

# SUP-676: was 10.0.0.0/16 -- identical to the product's own vpc-dev, which
# made peering the two VPCs impossible (AWS rejects a peering connection
# outright when the VPCs' primary CIDRs overlap, confirmed empirically; a
# non-overlapping secondary CIDR on one side does NOT help, despite
# appearances to the contrary in AWS's own secondary-CIDR-blocks examples).
# Moved off 10.0.0.0/16 entirely so real peering + normal SG-to-SG
# references work with no workarounds.
variable "vpc_cidr" {
  type        = string
  default     = "10.20.0.0/16"
  description = "CIDR block for the VPC"
}

variable "vpc_public_subnets" {
  type        = list(string)
  default     = ["10.20.1.0/24", "10.20.2.0/24", "10.20.3.0/24"]
  description = "CIDRs for the public subnets in the VPC, at least three are required"
}

variable "vpc_private_subnets" {
  type        = list(string)
  default     = ["10.20.11.0/24", "10.20.12.0/24", "10.20.13.0/24", "10.20.14.0/24", "10.20.15.0/24", "10.20.16.0/24"]
  description = "CIDRs for the private subnets in the VPC, at least three are required"
}

variable "vpc_elasticache_subnets" {
  type    = list(string)
  default = ["10.20.21.0/24", "10.20.22.0/24", "10.20.23.0/24"]
}

variable "vpc_availability_zones" {
  type        = list(string)
  description = "List of availability zones to use for the VPC subnets"
}

variable "vpc_endpoint_ingress_subnet_ids" {
  type = list(string)
}

variable "use_instance_connect" {
  type        = bool
  default     = true
  description = "Whether to deploy AWS EC2 Instance Connect Endpoint for SSH access to EC2 instances"
}
