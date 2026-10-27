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
  // Whether to serve monitoring endpoints (/checkpoint, /tile/*, /issuer/*) via a Cloud CDN-backed GCS backend bucket.
  enable_cdn             = optional(bool, false)
  // Optional GCS bucket name override if the bucket is not named [basename].[submission_host_suffix].
  bucket_name            = optional(string)
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
