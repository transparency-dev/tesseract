variable "project_id" {
  description = "GCP project ID where the log is hosted."
  type        = string
}

variable "logs" {
  description = "Map of log names to regions."
  type = map(object({
  region                 = string
  submission_host_suffix = string
  }))
}

variable "enable_cloud_armor" {
  description = "Whether or not to enable Cloud Armor for the load balancer."
  type        = bool
  default     = false
}
