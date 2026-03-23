# =============================================================================
# Auto Scaling Policies
# =============================================================================

# -----------------------------------------------------------------------------
# Control Plane Auto Scaling
# -----------------------------------------------------------------------------

resource "aws_autoscaling_policy" "control_plane_scale_up" {
  count = var.enable_autoscaling_policies ? 1 : 0

  name                   = "${var.prefix}-${var.environment}-cp-scale-up"
  autoscaling_group_name = aws_autoscaling_group.control_plane.name
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = 1
  cooldown               = 300
}

resource "aws_autoscaling_policy" "control_plane_scale_down" {
  count = var.enable_autoscaling_policies ? 1 : 0

  name                   = "${var.prefix}-${var.environment}-cp-scale-down"
  autoscaling_group_name = aws_autoscaling_group.control_plane.name
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = -1
  cooldown               = 300
}

resource "aws_cloudwatch_metric_alarm" "control_plane_cpu_high" {
  count = var.enable_autoscaling_policies ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-cp-cpu-high"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 120
  statistic           = "Average"
  threshold           = var.control_plane_scale_up_threshold

  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.control_plane.name
  }

  alarm_actions = [aws_autoscaling_policy.control_plane_scale_up[0].arn]

  tags = var.tags
}

resource "aws_cloudwatch_metric_alarm" "control_plane_cpu_low" {
  count = var.enable_autoscaling_policies ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-cp-cpu-low"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 3
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 120
  statistic           = "Average"
  threshold           = var.control_plane_scale_down_threshold

  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.control_plane.name
  }

  alarm_actions = [aws_autoscaling_policy.control_plane_scale_down[0].arn]

  tags = var.tags
}

# Target Tracking for Control Plane (Alternative)
resource "aws_autoscaling_policy" "control_plane_target_tracking" {
  count = var.enable_target_tracking_scaling ? 1 : 0

  name                   = "${var.prefix}-${var.environment}-cp-target-tracking"
  autoscaling_group_name = aws_autoscaling_group.control_plane.name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ASGAverageCPUUtilization"
    }
    target_value = var.control_plane_target_cpu_utilization
  }
}

# ALB Request Count Based Scaling for Control Plane
resource "aws_autoscaling_policy" "control_plane_request_count" {
  count = var.enable_request_count_scaling ? 1 : 0

  name                   = "${var.prefix}-${var.environment}-cp-request-count"
  autoscaling_group_name = aws_autoscaling_group.control_plane.name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ALBRequestCountPerTarget"
      resource_label         = "${aws_lb.api.arn_suffix}/${aws_lb_target_group.api.arn_suffix}"
    }
    target_value = var.control_plane_target_requests_per_instance
  }
}

# -----------------------------------------------------------------------------
# Worker Auto Scaling
# -----------------------------------------------------------------------------

resource "aws_autoscaling_policy" "worker_scale_up" {
  count = var.enable_autoscaling_policies ? 1 : 0

  name                   = "${var.prefix}-${var.environment}-worker-scale-up"
  autoscaling_group_name = aws_autoscaling_group.workers.name
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = 1
  cooldown               = 300
}

resource "aws_autoscaling_policy" "worker_scale_down" {
  count = var.enable_autoscaling_policies ? 1 : 0

  name                   = "${var.prefix}-${var.environment}-worker-scale-down"
  autoscaling_group_name = aws_autoscaling_group.workers.name
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = -1
  cooldown               = 600 # Longer cooldown for workers to allow sandbox cleanup
}

resource "aws_cloudwatch_metric_alarm" "worker_cpu_high" {
  count = var.enable_autoscaling_policies ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-worker-cpu-high"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 120
  statistic           = "Average"
  threshold           = var.worker_scale_up_threshold

  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.workers.name
  }

  alarm_actions = [aws_autoscaling_policy.worker_scale_up[0].arn]

  tags = var.tags
}

resource "aws_cloudwatch_metric_alarm" "worker_cpu_low" {
  count = var.enable_autoscaling_policies ? 1 : 0

  alarm_name          = "${var.prefix}-${var.environment}-worker-cpu-low"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 5 # More evaluation periods to avoid premature scale-down
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 120
  statistic           = "Average"
  threshold           = var.worker_scale_down_threshold

  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.workers.name
  }

  alarm_actions = [aws_autoscaling_policy.worker_scale_down[0].arn]

  tags = var.tags
}

# Target Tracking for Workers
resource "aws_autoscaling_policy" "worker_target_tracking" {
  count = var.enable_target_tracking_scaling ? 1 : 0

  name                   = "${var.prefix}-${var.environment}-worker-target-tracking"
  autoscaling_group_name = aws_autoscaling_group.workers.name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ASGAverageCPUUtilization"
    }
    target_value = var.worker_target_cpu_utilization
  }
}

# -----------------------------------------------------------------------------
# Scheduled Scaling (Optional - for predictable workloads)
# -----------------------------------------------------------------------------

resource "aws_autoscaling_schedule" "control_plane_scale_up_morning" {
  count = var.enable_scheduled_scaling ? 1 : 0

  scheduled_action_name  = "${var.prefix}-${var.environment}-cp-scale-up-morning"
  autoscaling_group_name = aws_autoscaling_group.control_plane.name
  min_size               = var.control_plane_min_size
  max_size               = var.control_plane_max_size
  desired_capacity       = var.control_plane_scheduled_peak_capacity
  recurrence             = var.scheduled_scale_up_cron
  time_zone              = var.scheduled_scaling_timezone
}

resource "aws_autoscaling_schedule" "control_plane_scale_down_evening" {
  count = var.enable_scheduled_scaling ? 1 : 0

  scheduled_action_name  = "${var.prefix}-${var.environment}-cp-scale-down-evening"
  autoscaling_group_name = aws_autoscaling_group.control_plane.name
  min_size               = var.control_plane_min_size
  max_size               = var.control_plane_max_size
  desired_capacity       = var.control_plane_scheduled_offpeak_capacity
  recurrence             = var.scheduled_scale_down_cron
  time_zone              = var.scheduled_scaling_timezone
}

resource "aws_autoscaling_schedule" "worker_scale_up_morning" {
  count = var.enable_scheduled_scaling ? 1 : 0

  scheduled_action_name  = "${var.prefix}-${var.environment}-worker-scale-up-morning"
  autoscaling_group_name = aws_autoscaling_group.workers.name
  min_size               = var.worker_min_size
  max_size               = var.worker_max_size
  desired_capacity       = var.worker_scheduled_peak_capacity
  recurrence             = var.scheduled_scale_up_cron
  time_zone              = var.scheduled_scaling_timezone
}

resource "aws_autoscaling_schedule" "worker_scale_down_evening" {
  count = var.enable_scheduled_scaling ? 1 : 0

  scheduled_action_name  = "${var.prefix}-${var.environment}-worker-scale-down-evening"
  autoscaling_group_name = aws_autoscaling_group.workers.name
  min_size               = var.worker_min_size
  max_size               = var.worker_max_size
  desired_capacity       = var.worker_scheduled_offpeak_capacity
  recurrence             = var.scheduled_scale_down_cron
  time_zone              = var.scheduled_scaling_timezone
}
