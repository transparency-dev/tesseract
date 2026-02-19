variable "project_id" {
  description = "GCP project ID where the log is hosted."
  type        = string
}

variable "logs" {
  description = "Map of log names to regions."
  type = map(object({
  // Region in which the backends are
  region                 = string
  // origin = [basename].[submission_host_suffix]
  submission_host_suffix = string
  }))

  validation {
    condition     = alltrue([
      for name, v in var.logs: v.region != "" && v.submission_host_suffix != "" && !startswith(v.submission_host_suffix, ".")
    ])
    error_message = "Both the region and submission_host_suffix must be set for each log. submission_host_suffix must not start with a \".\""
  }
}

variable "enable_cloud_armor" {
  description = "Whether or not to enable Cloud Armor for the load balancer."
  type        = bool
  default     = false
}
