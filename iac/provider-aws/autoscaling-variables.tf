# =============================================================================
# Variables for Auto Scaling Policies
# =============================================================================

# -----------------------------------------------------------------------------
# General Auto Scaling Settings
# -----------------------------------------------------------------------------

variable "enable_autoscaling_policies" {
  description = "Enable step scaling policies based on CPU utilization."
  type        = bool
  default     = true
}

variable "enable_target_tracking_scaling" {
  description = "Enable target tracking scaling policies. Mutually exclusive with step scaling for same metric."
  type        = bool
  default     = false
}

variable "enable_request_count_scaling" {
  description = "Enable ALB request count based scaling for control plane."
  type        = bool
  default     = false
}

variable "enable_scheduled_scaling" {
  description = "Enable scheduled scaling for predictable workloads."
  type        = bool
  default     = false
}

# -----------------------------------------------------------------------------
# Control Plane Scaling Thresholds
# -----------------------------------------------------------------------------

variable "control_plane_scale_up_threshold" {
  description = "CPU utilization percentage to trigger scale up for control plane."
  type        = number
  default     = 70
}

variable "control_plane_scale_down_threshold" {
  description = "CPU utilization percentage to trigger scale down for control plane."
  type        = number
  default     = 30
}

variable "control_plane_target_cpu_utilization" {
  description = "Target CPU utilization for control plane target tracking."
  type        = number
  default     = 60
}

variable "control_plane_target_requests_per_instance" {
  description = "Target requests per instance for ALB request count scaling."
  type        = number
  default     = 1000
}

# -----------------------------------------------------------------------------
# Worker Scaling Thresholds
# -----------------------------------------------------------------------------

variable "worker_scale_up_threshold" {
  description = "CPU utilization percentage to trigger scale up for workers."
  type        = number
  default     = 60
}

variable "worker_scale_down_threshold" {
  description = "CPU utilization percentage to trigger scale down for workers."
  type        = number
  default     = 25
}

variable "worker_target_cpu_utilization" {
  description = "Target CPU utilization for worker target tracking."
  type        = number
  default     = 50
}

# -----------------------------------------------------------------------------
# Scheduled Scaling Settings
# -----------------------------------------------------------------------------

variable "scheduled_scale_up_cron" {
  description = "Cron expression for scaling up (e.g., weekday mornings)."
  type        = string
  default     = "0 8 * * MON-FRI"
}

variable "scheduled_scale_down_cron" {
  description = "Cron expression for scaling down (e.g., weekday evenings)."
  type        = string
  default     = "0 20 * * MON-FRI"
}

variable "scheduled_scaling_timezone" {
  description = "Timezone for scheduled scaling (e.g., America/New_York, UTC)."
  type        = string
  default     = "UTC"
}

variable "control_plane_scheduled_peak_capacity" {
  description = "Desired capacity for control plane during peak hours."
  type        = number
  default     = 3
}

variable "control_plane_scheduled_offpeak_capacity" {
  description = "Desired capacity for control plane during off-peak hours."
  type        = number
  default     = 1
}

variable "worker_scheduled_peak_capacity" {
  description = "Desired capacity for workers during peak hours."
  type        = number
  default     = 5
}

variable "worker_scheduled_offpeak_capacity" {
  description = "Desired capacity for workers during off-peak hours."
  type        = number
  default     = 2
}
