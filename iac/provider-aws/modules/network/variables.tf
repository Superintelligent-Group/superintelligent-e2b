variable "prefix" {
  type = string
}

variable "vpc_cidr" {
  type        = string
  default     = "10.0.0.0/16"
  description = "CIDR block for the VPC"
}

variable "vpc_public_subnets" {
  type        = list(string)
  default     = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
  description = "CIDRs for the public subnets in the VPC, at least three are required"
}

variable "vpc_private_subnets" {
  type        = list(string)
  default     = ["10.0.11.0/24", "10.0.12.0/24", "10.0.13.0/24", "10.0.14.0/24", "10.0.15.0/24", "10.0.16.0/24"]
  description = "CIDRs for the private subnets in the VPC, at least three are required"
}

variable "vpc_elasticache_subnets" {
  type    = list(string)
  default = ["10.0.21.0/24", "10.0.22.0/24", "10.0.23.0/24"]
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

# SUP-676: the product's own app stack (RDS/Redis) lives in a separate VPC
# ("vpc-dev") that happens to share this VPC's exact primary CIDR
# (10.0.0.0/16) -- AWS refuses to peer VPCs whose CIDRs fully overlap. AWS
# does allow peering when at least one pair of the two VPCs' associated CIDR
# blocks is non-overlapping, so we associate a second, disjoint block here
# and carve the node pools' cross-VPC-reachable subnets from it. The
# existing 10.0.0.0/16 subnets/route table are untouched.
variable "vpc_peering_cidr" {
  type        = string
  default     = "10.1.0.0/16"
  description = "Secondary CIDR block associated with this VPC solely to enable peering with vpc-dev (whose primary CIDR overlaps this VPC's primary CIDR)"
}

variable "vpc_peering_subnets" {
  type = map(string)
  default = {
    "us-east-1a" = "10.1.11.0/24"
    "us-east-1b" = "10.1.12.0/24"
    "us-east-1c" = "10.1.13.0/24"
  }
  description = "AZ -> CIDR for the peering-only subnets carved from vpc_peering_cidr"
}
