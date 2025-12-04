variable "aws_region" {
  description = "AWS region to deploy the E2B cluster into."
  type        = string
}

variable "prefix" {
  description = "Prefix applied to names of created resources."
  type        = string
  default     = "e2b"
}

variable "environment" {
  description = "Environment name (prod, staging, dev)."
  type        = string
  default     = "prod"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC."
  type        = string
  default     = "10.10.0.0/16"
}

variable "availability_zones" {
  description = "List of availability zones to spread subnets across."
  type        = list(string)
}

variable "public_subnet_cidrs" {
  description = "CIDR blocks for public subnets; must align with availability_zones."
  type        = list(string)
}

variable "private_subnet_cidrs" {
  description = "CIDR blocks for private subnets; must align with availability_zones."
  type        = list(string)
}

variable "domain_name" {
  description = "Primary domain used for the E2B control plane (e.g. e2b.example.com)."
  type        = string
}

variable "acm_certificate_arn" {
  description = "ACM certificate ARN for the control-plane domain."
  type        = string
}

variable "create_route53_record" {
  description = "Whether to manage the DNS record in Route 53."
  type        = bool
  default     = false
}

variable "route53_zone_id" {
  description = "Route 53 hosted zone ID when create_route53_record is true."
  type        = string
  default     = ""
}

variable "cloudflare_api_token" {
  description = "Cloudflare API token if you prefer Cloudflare DNS. Leave empty to skip Cloudflare management."
  type        = string
  default     = ""
}

variable "cloudflare_zone_id" {
  description = "Cloudflare zone ID for managing DNS."
  type        = string
  default     = ""
}

variable "template_bucket_name" {
  description = "Name for the S3 bucket storing template rootfs artifacts."
  type        = string
  default     = ""
}

variable "snapshot_bucket_name" {
  description = "Name for the S3 bucket storing sandbox snapshots."
  type        = string
  default     = ""
}

variable "build_bucket_name" {
  description = "Name for the S3 bucket storing build artifacts (kernels, rootfs)."
  type        = string
  default     = ""
}

variable "log_bucket_name" {
  description = "Name for the S3 bucket storing exported logs."
  type        = string
  default     = ""
}

variable "enable_bucket_versioning" {
  description = "Enable versioning on artifact buckets."
  type        = bool
  default     = true
}

variable "ecr_repository_name" {
  description = "Base name for the main ECR repository that will host control-plane and worker images."
  type        = string
  default     = "e2b"
}

variable "custom_envs_repository_name" {
  description = "Name for the ECR repository that stores custom environment images."
  type        = string
  default     = "e2b-custom-envs"
}

variable "tags" {
  description = "Common tags to apply to resources."
  type        = map(string)
  default     = {}
}

variable "control_plane_ami_id" {
  description = "AMI ID for control-plane instances."
  type        = string
}

variable "control_plane_instance_type" {
  description = "Instance type for control-plane nodes."
  type        = string
  default     = "c6a.large"
}

variable "control_plane_min_size" {
  description = "Minimum number of control-plane instances."
  type        = number
  default     = 1
}

variable "control_plane_desired_capacity" {
  description = "Desired number of control-plane instances."
  type        = number
  default     = 2
}

variable "control_plane_max_size" {
  description = "Maximum number of control-plane instances."
  type        = number
  default     = 3
}

variable "control_plane_target_port" {
  description = "Port on the control-plane instances that the ALB should forward to."
  type        = number
  default     = 8080
}

variable "control_plane_user_data" {
  description = "User data script for control-plane instances."
  type        = string
  default     = ""
}

variable "worker_ami_id" {
  description = "AMI ID for worker instances (must be Nitro-capable for Firecracker)."
  type        = string
}

variable "worker_instance_type" {
  description = "Instance type for worker nodes."
  type        = string
  default     = "c6a.large"
}

variable "worker_min_size" {
  description = "Minimum number of worker instances."
  type        = number
  default     = 1
}

variable "worker_desired_capacity" {
  description = "Desired number of worker instances."
  type        = number
  default     = 2
}

variable "worker_max_size" {
  description = "Maximum number of worker instances."
  type        = number
  default     = 5
}

variable "worker_user_data" {
  description = "User data script for worker instances."
  type        = string
  default     = ""
}

variable "ssh_key_name" {
  description = "Optional EC2 key pair name to attach to instances for debugging."
  type        = string
  default     = ""
}
