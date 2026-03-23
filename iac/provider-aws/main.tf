locals {
  resolved_template_bucket = coalesce(var.template_bucket_name, "${var.prefix}-${var.environment}-templates")
  resolved_snapshot_bucket = coalesce(var.snapshot_bucket_name, "${var.prefix}-${var.environment}-snapshots")
  resolved_build_bucket    = coalesce(var.build_bucket_name, "${var.prefix}-${var.environment}-builds")
  resolved_log_bucket      = coalesce(var.log_bucket_name, "${var.prefix}-${var.environment}-logs")
  enable_cloudflare        = var.cloudflare_zone_id != "" && var.cloudflare_api_token != ""
  ssm_managed_instance_core = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = [
          "ssm:DescribeAssociation",
          "ssm:GetDeployablePatchSnapshotForInstance",
          "ssm:GetDocument",
          "ssm:DescribeDocument",
          "ssm:GetManifest",
          "ssm:GetParameters",
          "ssm:ListAssociations",
          "ssm:ListInstanceAssociations",
          "ssm:PutInventory",
          "ssm:PutComplianceItems",
          "ssm:PutConfigurePackageResult",
          "ssm:UpdateAssociationStatus",
          "ssm:UpdateInstanceAssociationStatus",
          "ssm:UpdateInstanceInformation",
          "ssmmessages:CreateControlChannel",
          "ssmmessages:CreateDataChannel",
          "ssmmessages:OpenControlChannel",
          "ssmmessages:OpenDataChannel",
          "ec2messages:AcknowledgeMessage",
          "ec2messages:DeleteMessage",
          "ec2messages:FailMessage",
          "ec2messages:GetEndpoint",
          "ec2messages:GetMessages",
          "ec2messages:SendReply"
        ]
        Resource = "*"
      }
    ]
  })
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.5.2"

  name = "${var.prefix}-${var.environment}-vpc"
  cidr = var.vpc_cidr

  azs             = var.availability_zones
  public_subnets  = var.public_subnet_cidrs
  private_subnets = var.private_subnet_cidrs

  enable_nat_gateway   = true
  single_nat_gateway   = true
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = var.tags
}

resource "aws_s3_bucket" "artifact_buckets" {
  for_each = {
    templates = local.resolved_template_bucket
    snapshots = local.resolved_snapshot_bucket
    builds    = local.resolved_build_bucket
    logs      = local.resolved_log_bucket
  }

  bucket        = each.value
  force_destroy = false

  tags = merge(var.tags, {
    "Name" = "${var.prefix}-${var.environment}-${each.key}"
  })
}

resource "aws_s3_bucket_public_access_block" "artifact_buckets" {
  for_each = aws_s3_bucket.artifact_buckets

  bucket = each.value.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_versioning" "artifact_buckets" {
  for_each = aws_s3_bucket.artifact_buckets

  bucket = each.value.id

  versioning_configuration {
    status = var.enable_bucket_versioning ? "Enabled" : "Suspended"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "artifact_buckets" {
  for_each = aws_s3_bucket.artifact_buckets

  bucket = each.value.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_ecr_repository" "orchestration" {
  name                 = var.ecr_repository_name
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = var.tags
}

resource "aws_ecr_repository" "custom_envs" {
  name                 = var.custom_envs_repository_name
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = var.tags
}

resource "aws_iam_role" "worker" {
  name = "${var.prefix}-${var.environment}-worker"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action    = "sts:AssumeRole"
        Effect    = "Allow"
        Principal = { Service = "ec2.amazonaws.com" }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role" "control_plane" {
  name = "${var.prefix}-${var.environment}-control-plane"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action    = "sts:AssumeRole"
        Effect    = "Allow"
        Principal = { Service = "ec2.amazonaws.com" }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy" "worker_bucket_access" {
  name = "${var.prefix}-${var.environment}-worker-buckets"
  role = aws_iam_role.worker.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:ListBucket"
        ],
        Resource = concat(
          [for bucket in aws_s3_bucket.artifact_buckets : bucket.arn],
          [for bucket in aws_s3_bucket.artifact_buckets : "${bucket.arn}/*"]
        )
      },
      {
        Effect = "Allow",
        Action = [
          "ecr:GetAuthorizationToken",
          "ecr:BatchCheckLayerAvailability",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:DescribeImages",
          "ecr:DescribeRepositories",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:PutImage"
        ],
        Resource = "*"
      },
      {
        Effect = "Allow",
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ],
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_role_policy" "worker_ssm" {
  name = "${var.prefix}-${var.environment}-worker-ssm"
  role = aws_iam_role.worker.id

  policy = local.ssm_managed_instance_core
}

resource "aws_iam_role_policy" "control_plane_bucket_access" {
  name = "${var.prefix}-${var.environment}-control-plane-buckets"
  role = aws_iam_role.control_plane.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:ListBucket"
        ],
        Resource = concat(
          [for bucket in aws_s3_bucket.artifact_buckets : bucket.arn],
          [for bucket in aws_s3_bucket.artifact_buckets : "${bucket.arn}/*"]
        )
      },
      {
        Effect = "Allow",
        Action = [
          "ecr:GetAuthorizationToken",
          "ecr:BatchCheckLayerAvailability",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:DescribeImages",
          "ecr:DescribeRepositories",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:PutImage"
        ],
        Resource = "*"
      },
      {
        Effect = "Allow",
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ],
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_role_policy" "control_plane_ssm" {
  name = "${var.prefix}-${var.environment}-control-plane-ssm"
  role = aws_iam_role.control_plane.id

  policy = local.ssm_managed_instance_core
}

resource "aws_iam_instance_profile" "worker" {
  name = "${var.prefix}-${var.environment}-worker"
  role = aws_iam_role.worker.name
}

resource "aws_iam_instance_profile" "control_plane" {
  name = "${var.prefix}-${var.environment}-control-plane"
  role = aws_iam_role.control_plane.name
}

resource "aws_route53_record" "domain" {
  count   = var.create_route53_record ? 1 : 0
  zone_id = var.route53_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_lb.api.dns_name
    zone_id                = aws_lb.api.zone_id
    evaluate_target_health = true
  }
}

resource "cloudflare_record" "domain" {
  count    = local.enable_cloudflare ? 1 : 0
  provider = cloudflare.managed

  zone_id = var.cloudflare_zone_id
  name    = var.domain_name
  type    = "CNAME"
  value   = aws_lb.api.dns_name
  proxied = false
}

resource "aws_lb" "api" {
  name               = "${var.prefix}-${var.environment}-api"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.api.id]
  subnets            = module.vpc.public_subnets

  tags = var.tags
}

resource "aws_lb_listener" "http_redirect" {
  load_balancer_arn = aws_lb.api.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

resource "aws_lb_target_group" "api" {
  name        = "${var.prefix}-${var.environment}-api"
  port        = var.control_plane_target_port
  protocol    = "HTTP"
  vpc_id      = module.vpc.vpc_id
  target_type = "instance"

  health_check {
    matcher = "200"
    path    = "/healthz"
  }

  tags = var.tags
}

resource "aws_security_group" "api" {
  name        = "${var.prefix}-${var.environment}-alb"
  description = "Allow HTTPS ingress to the E2B control-plane load balancer"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description = "HTTP redirect"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = var.tags
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.api.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = var.acm_certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }
}

resource "aws_security_group" "control_plane" {
  name        = "${var.prefix}-${var.environment}-control-plane"
  description = "Allow control-plane instances to receive traffic from ALB and egress to the internet"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "ALB to API"
    from_port       = var.control_plane_target_port
    to_port         = var.control_plane_target_port
    protocol        = "tcp"
    security_groups = [aws_security_group.api.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = var.tags
}

resource "aws_security_group" "workers" {
  name        = "${var.prefix}-${var.environment}-workers"
  description = "Allow worker nodes to receive traffic from the control plane and egress"
  vpc_id      = module.vpc.vpc_id

  ingress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = [module.vpc.vpc_cidr_block]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = var.tags
}

resource "aws_launch_template" "control_plane" {
  name_prefix   = "${var.prefix}-${var.environment}-cp-"
  image_id      = var.control_plane_ami_id
  instance_type = var.control_plane_instance_type
  key_name      = var.ssh_key_name

  iam_instance_profile {
    name = aws_iam_instance_profile.control_plane.name
  }

  vpc_security_group_ids = [aws_security_group.control_plane.id]
  user_data              = base64encode(local.final_control_plane_userdata)

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size = var.root_volume_size
      volume_type = "gp3"
      encrypted   = true
      kms_key_id  = var.root_volume_kms_key_id != "" ? var.root_volume_kms_key_id : null
    }
  }

  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 2
  }

  tag_specifications {
    resource_type = "instance"
    tags          = merge(var.tags, { Name = "${var.prefix}-${var.environment}-control-plane" })
  }
}

resource "aws_launch_template" "worker" {
  name_prefix   = "${var.prefix}-${var.environment}-worker-"
  image_id      = var.worker_ami_id
  instance_type = var.worker_instance_type
  key_name      = var.ssh_key_name

  iam_instance_profile {
    name = aws_iam_instance_profile.worker.name
  }

  vpc_security_group_ids = [aws_security_group.workers.id]
  user_data              = base64encode(local.final_worker_userdata)

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size = var.root_volume_size
      volume_type = "gp3"
      encrypted   = true
      kms_key_id  = var.root_volume_kms_key_id != "" ? var.root_volume_kms_key_id : null
    }
  }

  metadata_options {
    http_tokens                 = "required"
    http_put_response_hop_limit = 2
  }

  tag_specifications {
    resource_type = "instance"
    tags          = merge(var.tags, { Name = "${var.prefix}-${var.environment}-worker" })
  }
}

resource "aws_autoscaling_group" "control_plane" {
  name                      = "${var.prefix}-${var.environment}-control-plane"
  desired_capacity          = var.control_plane_desired_capacity
  max_size                  = var.control_plane_max_size
  min_size                  = var.control_plane_min_size
  vpc_zone_identifier       = module.vpc.private_subnets
  health_check_type         = "ELB"
  health_check_grace_period = 300

  launch_template {
    id      = aws_launch_template.control_plane.id
    version = "$Latest"
  }

  target_group_arns = [aws_lb_target_group.api.arn]

  tag {
    key                 = "Name"
    value               = "${var.prefix}-${var.environment}-control-plane"
    propagate_at_launch = true
  }
}

resource "aws_autoscaling_group" "workers" {
  name                = "${var.prefix}-${var.environment}-workers"
  desired_capacity    = var.worker_desired_capacity
  max_size            = var.worker_max_size
  min_size            = var.worker_min_size
  vpc_zone_identifier = module.vpc.private_subnets

  launch_template {
    id      = aws_launch_template.worker.id
    version = "$Latest"
  }

  tag {
    key                 = "Name"
    value               = "${var.prefix}-${var.environment}-worker"
    propagate_at_launch = true
  }
}

output "vpc_id" {
  value       = module.vpc.vpc_id
  description = "ID of the created VPC."
}

output "public_subnet_ids" {
  value       = module.vpc.public_subnets
  description = "IDs of public subnets for the ALB."
}

output "private_subnet_ids" {
  value       = module.vpc.private_subnets
  description = "IDs of private subnets for control-plane and workers."
}

output "artifact_buckets" {
  value = {
    templates = local.resolved_template_bucket
    snapshots = local.resolved_snapshot_bucket
    builds    = local.resolved_build_bucket
    logs      = local.resolved_log_bucket
  }
  description = "S3 bucket names for templates, snapshots, builds, and logs."
}

output "ecr_repositories" {
  value = {
    orchestration = aws_ecr_repository.orchestration.repository_url
    custom_envs   = aws_ecr_repository.custom_envs.repository_url
  }
  description = "ECR repositories for core services and custom environments."
}

output "worker_iam_role_arn" {
  value       = aws_iam_role.worker.arn
  description = "IAM role ARN for worker nodes to pull artifacts and push logs."
}

output "control_plane_iam_role_arn" {
  value       = aws_iam_role.control_plane.arn
  description = "IAM role ARN for control-plane instances."
}

output "control_plane_security_group_id" {
  value       = aws_security_group.control_plane.id
  description = "Security group ID for control-plane instances."
}

output "worker_security_group_id" {
  value       = aws_security_group.workers.id
  description = "Security group ID for worker instances."
}

output "control_plane_autoscaling_group" {
  value       = aws_autoscaling_group.control_plane.name
  description = "Autoscaling group name for control-plane instances."
}

output "worker_autoscaling_group" {
  value       = aws_autoscaling_group.workers.name
  description = "Autoscaling group name for worker instances."
}

output "alb_dns_name" {
  value       = aws_lb.api.dns_name
  description = "DNS name of the Application Load Balancer that fronts the control plane."
}
