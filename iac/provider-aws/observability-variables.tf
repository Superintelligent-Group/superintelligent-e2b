# =============================================================================
# Variables for CloudWatch Observability
# =============================================================================

variable "create_observability" {
  description = "Whether to create CloudWatch log groups, metrics, and alarms."
  type        = bool
  default     = true
}

variable "log_retention_days" {
  description = "Number of days to retain CloudWatch logs."
  type        = number
  default     = 30
}

variable "enable_alb_access_logs" {
  description = "Enable ALB access logging to S3."
  type        = bool
  default     = true
}

variable "create_alarms" {
  description = "Whether to create CloudWatch alarms."
  type        = bool
  default     = true
}

variable "alarm_email" {
  description = "Email address to receive alarm notifications. Leave empty to skip email subscription."
  type        = string
  default     = ""
}

variable "alarm_5xx_threshold" {
  description = "Threshold for 5xx error count alarm."
  type        = number
  default     = 10
}

variable "alarm_latency_threshold_seconds" {
  description = "Threshold for p99 latency alarm in seconds."
  type        = number
  default     = 5
}

variable "create_dashboard" {
  description = "Whether to create CloudWatch dashboard."
  type        = bool
  default     = true
}
