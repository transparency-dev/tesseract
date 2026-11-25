variable "project_id" {
  description = "GCP project ID where the log is hosted."
  type        = string
}

variable "logs" {
  description = "Map of log names to regions."
  type = map(object({
    // Region in which the backends are
    region = string
    // origin = [basename].[submission_host_suffix]
    submission_host_suffix = string
  }))

  validation {
    condition = alltrue([
      for name, v in var.logs : v.region != "" && v.submission_host_suffix != "" && !startswith(v.submission_host_suffix, ".")
    ])
    error_message = "Both the region and submission_host_suffix must be set for each log. submission_host_suffix must not start with a \".\""
  }
}

variable "enable_cloud_armor" {
  description = "Whether or not to enable Cloud Armor for the load balancer."
  type        = bool
  default     = false
}

variable "instance_group_names" {
  description = "Optional map of log name to the list of regional instance group names backing that log's backend service (for example during a blue/green MIG cutover). Defaults to [\"<log>-instance-group-manager\"] for any log not present in this map."
  type        = map(list(string))
  default     = {}
  nullable    = false

  validation {
    condition     = alltrue([for k, names in var.instance_group_names : length(names) > 0 && alltrue([for n in names : n != ""])])
    error_message = "Each entry in instance_group_names must be a non-empty list of non-empty instance group names."
  }
}

