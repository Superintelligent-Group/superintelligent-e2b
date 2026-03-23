# =============================================================================
# CloudWatch Observability: Log Groups, Metrics, Alarms, Dashboards
# =============================================================================

# -----------------------------------------------------------------------------
# CloudWatch Log Groups
# -----------------------------------------------------------------------------

resource "aws_cloudwatch_log_group" "api" {
  count = var.create_observability ? 1 : 0

  name              = "/e2b/${var.environment}/api"
  retention_in_days = var.log_retention_days

  tags = var.tags
}

resource "aws_cloudwatch_log_group" "orchestrator" {
  count = var.create_observability ? 1 : 0

  name              = "/e2b/${var.environment}/orchestrator"
  retention_in_days = var.log_retention_days

  tags = var.tags
}

resource "aws_cloudwatch_log_group" "client_proxy" {
  count = var.create_observability ? 1 : 0

  name              = "/e2b/${var.environment}/client-proxy"
  retention_in_days = var.log_retention_days

  tags = var.tags
}

resource "aws_cloudwatch_log_group" "template_manager" {
  count = var.create_observability ? 1 : 0

  name              = "/e2b/${var.environment}/template-manager"
  retention_in_days = var.log_retention_days

  tags = var.tags
}

resource "aws_cloudwatch_log_group" "nomad" {
  count = var.create_observability ? 1 : 0

  name              = "/e2b/${var.environment}/nomad"
  retention_in_days = var.log_retention_days

  tags = var.tags
}

resource "aws_cloudwatch_log_group" "consul" {
  count = var.create_observability ? 1 : 0

  name              = "/e2b/${var.environment}/consul"
  retention_in_days = var.log_retention_days

  tags = var.tags
}

# -----------------------------------------------------------------------------
# ALB Access Logging
# -----------------------------------------------------------------------------

resource "aws_s3_bucket_policy" "alb_logs" {
  count = var.create_observability && var.enable_alb_access_logs ? 1 : 0

  bucket = aws_s3_bucket.artifact_buckets["logs"].id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_elb_service_account.main.id}:root"
        }
        Action   = "s3:PutObject"
        Resource = "${aws_s3_bucket.artifact_buckets["logs"].arn}/alb-access-logs/*"
      }
    ]
  })
}

data "aws_elb_service_account" "main" {}

resource "aws_lb_attribute" "access_logs" {
  count = var.create_observability && var.enable_alb_access_logs ? 1 : 0

  load_balancer_arn = aws_lb.api.arn
  key               = "access_logs.s3.enabled"
  value             = "true"
}

resource "aws_lb_attribute" "access_logs_bucket" {
  count = var.create_observability && var.enable_alb_access_logs ? 1 : 0

  load_balancer_arn = aws_lb.api.arn
  key               = "access_logs.s3.bucket"
  value             = aws_s3_bucket.artifact_buckets["logs"].id
}

resource "aws_lb_attribute" "access_logs_prefix" {
  count = var.create_observability && var.enable_alb_access_logs ? 1 : 0

  load_balancer_arn = aws_lb.api.arn
  key               = "access_logs.s3.prefix"
  value             = "alb-access-logs"
}

# -----------------------------------------------------------------------------
# SNS Topic for Alarms
# -----------------------------------------------------------------------------

resource "aws_sns_topic" "alarms" {
  count = var.create_observability && var.create_alarms ? 1 : 0

  name = "${var.prefix}-${var.environment}-alarms"

  tags = var.tags
}

resource "aws_sns_topic_subscription" "alarms_email" {
  count = var.create_observability && var.create_alarms && var.alarm_email != "" ? 1 : 0

  topic_arn = aws_sns_topic.alarms[0].arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

# -----------------------------------------------------------------------------
# CloudWatch Alarms
# -----------------------------------------------------------------------------

# ALB 5xx Errors
resource "aws_cloudwatch_metric_alarm" "alb_5xx" {
  count = var.create_observability && var.create_alarms ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-alb-5xx-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "HTTPCode_ELB_5XX_Count"
  namespace           = "AWS/ApplicationELB"
  period              = 300
  statistic           = "Sum"
  threshold           = var.alarm_5xx_threshold
  alarm_description   = "ALB 5xx errors exceeded threshold"
  treat_missing_data  = "notBreaching"

  dimensions = {
    LoadBalancer = aws_lb.api.arn_suffix
  }

  alarm_actions = [aws_sns_topic.alarms[0].arn]
  ok_actions    = [aws_sns_topic.alarms[0].arn]

  tags = var.tags
}

# ALB Target 5xx Errors
resource "aws_cloudwatch_metric_alarm" "target_5xx" {
  count = var.create_observability && var.create_alarms ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-target-5xx-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "HTTPCode_Target_5XX_Count"
  namespace           = "AWS/ApplicationELB"
  period              = 300
  statistic           = "Sum"
  threshold           = var.alarm_5xx_threshold
  alarm_description   = "Target 5xx errors exceeded threshold"
  treat_missing_data  = "notBreaching"

  dimensions = {
    LoadBalancer = aws_lb.api.arn_suffix
    TargetGroup  = aws_lb_target_group.api.arn_suffix
  }

  alarm_actions = [aws_sns_topic.alarms[0].arn]
  ok_actions    = [aws_sns_topic.alarms[0].arn]

  tags = var.tags
}

# ALB Latency
resource "aws_cloudwatch_metric_alarm" "alb_latency" {
  count = var.create_observability && var.create_alarms ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-alb-high-latency"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "TargetResponseTime"
  namespace           = "AWS/ApplicationELB"
  period              = 300
  extended_statistic  = "p99"
  threshold           = var.alarm_latency_threshold_seconds
  alarm_description   = "ALB p99 latency exceeded threshold"
  treat_missing_data  = "notBreaching"

  dimensions = {
    LoadBalancer = aws_lb.api.arn_suffix
    TargetGroup  = aws_lb_target_group.api.arn_suffix
  }

  alarm_actions = [aws_sns_topic.alarms[0].arn]
  ok_actions    = [aws_sns_topic.alarms[0].arn]

  tags = var.tags
}

# Unhealthy Host Count
resource "aws_cloudwatch_metric_alarm" "unhealthy_hosts" {
  count = var.create_observability && var.create_alarms ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-unhealthy-hosts"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "UnHealthyHostCount"
  namespace           = "AWS/ApplicationELB"
  period              = 60
  statistic           = "Average"
  threshold           = 0
  alarm_description   = "Unhealthy hosts detected in target group"
  treat_missing_data  = "notBreaching"

  dimensions = {
    LoadBalancer = aws_lb.api.arn_suffix
    TargetGroup  = aws_lb_target_group.api.arn_suffix
  }

  alarm_actions = [aws_sns_topic.alarms[0].arn]
  ok_actions    = [aws_sns_topic.alarms[0].arn]

  tags = var.tags
}

# RDS CPU Utilization
resource "aws_cloudwatch_metric_alarm" "rds_cpu" {
  count = var.create_observability && var.create_alarms && var.create_rds ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-rds-high-cpu"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "CPUUtilization"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 80
  alarm_description   = "RDS CPU utilization exceeded 80%"
  treat_missing_data  = "notBreaching"

  dimensions = {
    DBInstanceIdentifier = aws_db_instance.postgres[0].identifier
  }

  alarm_actions = [aws_sns_topic.alarms[0].arn]
  ok_actions    = [aws_sns_topic.alarms[0].arn]

  tags = var.tags
}

# RDS Free Storage Space
resource "aws_cloudwatch_metric_alarm" "rds_storage" {
  count = var.create_observability && var.create_alarms && var.create_rds ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-rds-low-storage"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 2
  metric_name         = "FreeStorageSpace"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 5368709120 # 5 GB in bytes
  alarm_description   = "RDS free storage space below 5GB"
  treat_missing_data  = "notBreaching"

  dimensions = {
    DBInstanceIdentifier = aws_db_instance.postgres[0].identifier
  }

  alarm_actions = [aws_sns_topic.alarms[0].arn]
  ok_actions    = [aws_sns_topic.alarms[0].arn]

  tags = var.tags
}

# Redis CPU Utilization
resource "aws_cloudwatch_metric_alarm" "redis_cpu" {
  count = var.create_observability && var.create_alarms && var.create_elasticache ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-redis-high-cpu"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "CPUUtilization"
  namespace           = "AWS/ElastiCache"
  period              = 300
  statistic           = "Average"
  threshold           = 75
  alarm_description   = "Redis CPU utilization exceeded 75%"
  treat_missing_data  = "notBreaching"

  dimensions = {
    ReplicationGroupId = aws_elasticache_replication_group.redis[0].id
  }

  alarm_actions = [aws_sns_topic.alarms[0].arn]
  ok_actions    = [aws_sns_topic.alarms[0].arn]

  tags = var.tags
}

# Redis Memory Usage
resource "aws_cloudwatch_metric_alarm" "redis_memory" {
  count = var.create_observability && var.create_alarms && var.create_elasticache ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-redis-high-memory"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "DatabaseMemoryUsagePercentage"
  namespace           = "AWS/ElastiCache"
  period              = 300
  statistic           = "Average"
  threshold           = 80
  alarm_description   = "Redis memory usage exceeded 80%"
  treat_missing_data  = "notBreaching"

  dimensions = {
    ReplicationGroupId = aws_elasticache_replication_group.redis[0].id
  }

  alarm_actions = [aws_sns_topic.alarms[0].arn]
  ok_actions    = [aws_sns_topic.alarms[0].arn]

  tags = var.tags
}

# -----------------------------------------------------------------------------
# CloudWatch Dashboard
# -----------------------------------------------------------------------------

resource "aws_cloudwatch_dashboard" "main" {
  count = var.create_observability && var.create_dashboard ? 1 : 0

  dashboard_name = "${var.prefix}-${var.environment}-overview"

  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          title  = "ALB Request Count"
          region = var.aws_region
          metrics = [
            ["AWS/ApplicationELB", "RequestCount", "LoadBalancer", aws_lb.api.arn_suffix, { stat = "Sum", period = 60 }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          title  = "ALB Response Time"
          region = var.aws_region
          metrics = [
            ["AWS/ApplicationELB", "TargetResponseTime", "LoadBalancer", aws_lb.api.arn_suffix, "TargetGroup", aws_lb_target_group.api.arn_suffix, { stat = "p99", period = 60 }],
            ["...", { stat = "p50", period = 60 }],
            ["...", { stat = "Average", period = 60 }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 6
        width  = 12
        height = 6
        properties = {
          title  = "ALB HTTP Error Codes"
          region = var.aws_region
          metrics = [
            ["AWS/ApplicationELB", "HTTPCode_ELB_5XX_Count", "LoadBalancer", aws_lb.api.arn_suffix, { stat = "Sum", period = 60, color = "#d62728" }],
            [".", "HTTPCode_ELB_4XX_Count", ".", ".", { stat = "Sum", period = 60, color = "#ff7f0e" }],
            [".", "HTTPCode_Target_5XX_Count", ".", ".", "TargetGroup", aws_lb_target_group.api.arn_suffix, { stat = "Sum", period = 60, color = "#9467bd" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 6
        width  = 12
        height = 6
        properties = {
          title  = "Healthy/Unhealthy Hosts"
          region = var.aws_region
          metrics = [
            ["AWS/ApplicationELB", "HealthyHostCount", "LoadBalancer", aws_lb.api.arn_suffix, "TargetGroup", aws_lb_target_group.api.arn_suffix, { stat = "Average", period = 60, color = "#2ca02c" }],
            [".", "UnHealthyHostCount", ".", ".", ".", ".", { stat = "Average", period = 60, color = "#d62728" }]
          ]
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 12
        width  = 8
        height = 6
        properties = {
          title  = "RDS CPU & Connections"
          region = var.aws_region
          metrics = var.create_rds ? [
            ["AWS/RDS", "CPUUtilization", "DBInstanceIdentifier", aws_db_instance.postgres[0].identifier, { stat = "Average", period = 60 }],
            [".", "DatabaseConnections", ".", ".", { stat = "Average", period = 60, yAxis = "right" }]
          ] : []
        }
      },
      {
        type   = "metric"
        x      = 8
        y      = 12
        width  = 8
        height = 6
        properties = {
          title  = "Redis CPU & Memory"
          region = var.aws_region
          metrics = var.create_elasticache ? [
            ["AWS/ElastiCache", "CPUUtilization", "ReplicationGroupId", aws_elasticache_replication_group.redis[0].id, { stat = "Average", period = 60 }],
            [".", "DatabaseMemoryUsagePercentage", ".", ".", { stat = "Average", period = 60, yAxis = "right" }]
          ] : []
        }
      },
      {
        type   = "metric"
        x      = 16
        y      = 12
        width  = 8
        height = 6
        properties = {
          title  = "ASG Instance Count"
          region = var.aws_region
          metrics = [
            ["AWS/AutoScaling", "GroupInServiceInstances", "AutoScalingGroupName", aws_autoscaling_group.control_plane.name, { stat = "Average", period = 60, label = "Control Plane" }],
            [".", ".", ".", aws_autoscaling_group.workers.name, { stat = "Average", period = 60, label = "Workers" }]
          ]
        }
      }
    ]
  })
}

# -----------------------------------------------------------------------------
# Observability Outputs
# -----------------------------------------------------------------------------

output "log_groups" {
  value = var.create_observability ? {
    api              = aws_cloudwatch_log_group.api[0].name
    orchestrator     = aws_cloudwatch_log_group.orchestrator[0].name
    client_proxy     = aws_cloudwatch_log_group.client_proxy[0].name
    template_manager = aws_cloudwatch_log_group.template_manager[0].name
    nomad            = aws_cloudwatch_log_group.nomad[0].name
    consul           = aws_cloudwatch_log_group.consul[0].name
  } : {}
  description = "CloudWatch Log Group names"
}

output "sns_alarm_topic_arn" {
  value       = var.create_observability && var.create_alarms ? aws_sns_topic.alarms[0].arn : null
  description = "SNS topic ARN for alarms"
}

output "dashboard_url" {
  value       = var.create_observability && var.create_dashboard ? "https://${var.aws_region}.console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards:name=${aws_cloudwatch_dashboard.main[0].dashboard_name}" : null
  description = "CloudWatch dashboard URL"
}
