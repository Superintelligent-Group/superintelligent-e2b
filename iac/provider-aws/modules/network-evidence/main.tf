terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.35.1" }
  }
}

variable "account_id" { type = string }
variable "bucket_name" { type = string }
variable "evidence_prefix" { type = string }
variable "producer_role_arn" { type = string }
variable "reader_role_arn" { type = string }
variable "retention_days" { type = number }
variable "expiration_days" { type = number }

data "aws_caller_identity" "current" {}

locals {
  policy = templatefile("${path.module}/policy.json.tftpl", {
    bucket   = var.bucket_name
    account  = var.account_id
    prefix   = var.evidence_prefix
    producer = var.producer_role_arn
    reader   = var.reader_role_arn
  })
}

resource "aws_s3_bucket" "evidence" {
  bucket              = var.bucket_name
  object_lock_enabled = true
  force_destroy       = false
  lifecycle {
    prevent_destroy = true
    precondition {
      condition     = data.aws_caller_identity.current.account_id == var.account_id
      error_message = "Network evidence AWS account must match the explicit target."
    }
    precondition {
      condition     = can(regex("^[0-9]{12}$", var.account_id)) && can(regex("^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.bucket_name)) && can(regex("^network-usage/v1/[a-zA-Z0-9_-]+$", var.evidence_prefix))
      error_message = "An explicit account, bucket and dedicated safe evidence prefix are required."
    }
    precondition {
      condition     = var.producer_role_arn != var.reader_role_arn && can(regex("^arn:aws:iam::${var.account_id}:role/[a-zA-Z0-9_+=,.@/-]+$", var.producer_role_arn)) && can(regex("^arn:aws:iam::${var.account_id}:role/[a-zA-Z0-9_+=,.@/-]+$", var.reader_role_arn))
      error_message = "Distinct explicit producer and reader roles in the pinned account are required."
    }
    precondition {
      condition     = var.retention_days >= 1 && var.retention_days <= 36500 && floor(var.retention_days) == var.retention_days && var.expiration_days > var.retention_days && floor(var.expiration_days) == var.expiration_days
      error_message = "Explicit integral retention and later expiration durations are required."
    }
  }
}
resource "aws_s3_bucket_versioning" "evidence" {
  bucket = aws_s3_bucket.evidence.id
  versioning_configuration { status = "Enabled" }
}
resource "aws_s3_bucket_public_access_block" "evidence" {
  bucket                  = aws_s3_bucket.evidence.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
resource "aws_s3_bucket_ownership_controls" "evidence" {
  bucket = aws_s3_bucket.evidence.id
  rule { object_ownership = "BucketOwnerEnforced" }
}
resource "aws_s3_bucket_object_lock_configuration" "evidence" {
  bucket     = aws_s3_bucket.evidence.id
  depends_on = [aws_s3_bucket_versioning.evidence]
  rule {
    default_retention {
      mode = "GOVERNANCE"
      days = var.retention_days
    }
  }
}
resource "aws_s3_bucket_lifecycle_configuration" "evidence" {
  bucket     = aws_s3_bucket.evidence.id
  depends_on = [aws_s3_bucket_versioning.evidence, aws_s3_bucket_object_lock_configuration.evidence]
  rule {
    id     = "bounded-raw-evidence-retention"
    status = "Enabled"
    filter { prefix = "${var.evidence_prefix}/" }
    expiration { days = var.expiration_days }
    noncurrent_version_expiration { noncurrent_days = var.expiration_days }
  }
}
resource "aws_s3_bucket_policy" "evidence" {
  bucket     = aws_s3_bucket.evidence.id
  policy     = local.policy
  depends_on = [aws_s3_bucket_public_access_block.evidence]
}
output "policy_sha256" { value = sha256(jsonencode(jsondecode(local.policy))) }
output "policy_json" { value = local.policy }
output "bucket_name" { value = aws_s3_bucket.evidence.id }
