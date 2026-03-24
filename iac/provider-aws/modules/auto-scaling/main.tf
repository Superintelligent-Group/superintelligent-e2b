# =============================================================================
# E2B Auto-Scaling Module
# =============================================================================
# Provides automatic wake-up and shutdown for the E2B cluster.
#
# - Wake Lambda:     Called by swarm worker before sandbox creation.
#                    Scales up control → API → client with spot instances.
# - Shutdown Lambda: Runs every 5 min via EventBridge. Scales to zero
#                    after idle_timeout_minutes of no activity.
# =============================================================================

data "archive_file" "lambda" {
  type        = "zip"
  source_dir  = "${path.module}/lambda"
  output_path = "${path.module}/lambda.zip"
}

# --- IAM ---

resource "aws_iam_role" "scaler" {
  name = "${var.prefix}cluster-scaler"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })

  tags = var.tags
}

resource "aws_iam_role_policy" "scaler" {
  name = "${var.prefix}cluster-scaler"
  role = aws_iam_role.scaler.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "autoscaling:DescribeAutoScalingGroups",
          "autoscaling:UpdateAutoScalingGroup",
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ec2:RunInstances",
          "ec2:CreateTags",
          "ec2:DescribeLaunchTemplateVersions",
        ]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = ["iam:PassRole"]
        Resource = "*"
        Condition = {
          StringEquals = {
            "iam:PassedToService" = "ec2.amazonaws.com"
          }
        }
      },
      {
        Effect = "Allow"
        Action = [
          "cloudwatch:PutMetricData",
          "cloudwatch:GetMetricStatistics",
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ]
        Resource = "arn:aws:logs:*:*:*"
      },
    ]
  })
}

# --- Lambda Functions ---

locals {
  lambda_env = {
    CONTROL_SERVER_ASG_NAME  = var.control_server_asg_name
    API_ASG_NAME             = var.api_asg_name
    CLIENT_ASG_NAME          = var.client_asg_name
    BUILD_ASG_NAME           = var.build_asg_name
    IDLE_TIMEOUT_MINUTES     = tostring(var.idle_timeout_minutes)
    CLIENT_SPOT_INSTANCE_TYPES = jsonencode(var.client_spot_instance_types)
    API_SPOT_INSTANCE_TYPES    = jsonencode(var.api_spot_instance_types)
  }
}

resource "aws_lambda_function" "wake" {
  function_name    = "${var.prefix}cluster-wake"
  filename         = data.archive_file.lambda.output_path
  source_code_hash = data.archive_file.lambda.output_base64sha256
  handler          = "cluster_scaler.wake_handler"
  runtime          = "python3.12"
  role             = aws_iam_role.scaler.arn
  timeout          = 600  # 10 min — includes waiting for instances to boot
  memory_size      = 128

  environment {
    variables = local.lambda_env
  }

  tags = var.tags
}

resource "aws_lambda_function" "shutdown" {
  function_name    = "${var.prefix}cluster-shutdown"
  filename         = data.archive_file.lambda.output_path
  source_code_hash = data.archive_file.lambda.output_base64sha256
  handler          = "cluster_scaler.shutdown_handler"
  runtime          = "python3.12"
  role             = aws_iam_role.scaler.arn
  timeout          = 60
  memory_size      = 128

  environment {
    variables = local.lambda_env
  }

  tags = var.tags
}

# --- Lambda Function URL (public endpoint for wake-up) ---

resource "aws_lambda_function_url" "wake" {
  function_name      = aws_lambda_function.wake.function_name
  authorization_type = "NONE"  # Swarm worker calls this — secured by obscurity + idempotency
}

# --- EventBridge: Periodic Shutdown Check ---

resource "aws_cloudwatch_event_rule" "idle_check" {
  name                = "${var.prefix}cluster-idle-check"
  description         = "Check if E2B cluster is idle every 5 minutes"
  schedule_expression = "rate(5 minutes)"
  tags                = var.tags
}

resource "aws_cloudwatch_event_target" "idle_check" {
  rule = aws_cloudwatch_event_rule.idle_check.name
  arn  = aws_lambda_function.shutdown.arn
}

resource "aws_lambda_permission" "eventbridge_shutdown" {
  statement_id  = "AllowEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.shutdown.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.idle_check.arn
}
