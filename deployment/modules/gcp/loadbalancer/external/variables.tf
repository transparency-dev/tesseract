variable "project_id" {
  description = "GCP project ID where the log is hosted."
  type        = string
}

variable "logs" {
  description = "Map of log names to regions."
  type = map(string)
}

// TODO: this shouldn't be a list really, revert back to a single suffix.
variable "submission_host_suffixes" {
  description = "Submission host suffixes, appended to each log name. MUST cover all log origin suffixes as per https://c2sp.org/static-ct-api."
  type        = string
}

variable "enable_cloud_armor" {
  description = "Whether or not to enable Cloud Armor for the load balancer."
  type        = bool
  default     = false
}
