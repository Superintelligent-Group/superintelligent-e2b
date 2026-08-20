variable "prefix" {
  type = string
}

variable "project_tag" {
  type        = string
  description = "Explicit Project cost-attribution tag for E2B registry and secrets."
  default     = "e2b"

  validation {
    condition     = trimspace(var.project_tag) != ""
    error_message = "project_tag must be a non-empty cost-attribution label."
  }
}

variable "bucket_prefix" {
  type = string
}

variable "allow_force_destroy" {
  default = false
}

variable "region" {
  type = string
}

variable "endpoint_ingress_subnet_ids" {
  type = list(string)
}

# SUP-676: create_rds/rds_instance_class/etc were already declared in
# dev.cq.tfvars with real values but never wired to anything (same class of
# drift as vpc_cidr) -- e2b's own dedicated Postgres, so api/client-proxy
# never need to reach across into the product's shared vpc-dev RDS at all.
variable "create_rds" {
  type    = bool
  default = false
}

variable "rds_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "rds_allocated_storage" {
  type    = number
  default = 20
}

variable "rds_multi_az" {
  type    = bool
  default = false
}

variable "rds_performance_insights" {
  type    = bool
  default = false
}
