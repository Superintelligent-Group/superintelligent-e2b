variable "aws_region" {
  description = "AWS region used to create the remote state resources."
  type        = string
}

variable "state_bucket_name" {
  description = "Name of the S3 bucket that will hold Terraform state. Must be globally unique."
  type        = string
}

variable "lock_table_name" {
  description = "Name of the DynamoDB table used for Terraform state locking."
  type        = string
  default     = "terraform-locks"
}

variable "tags" {
  description = "Common tags applied to created resources."
  type        = map(string)
  default     = {}
}
