variable "project_id" {
  description = "GCP project ID where the log is hosted"
  type        = string
}

variable "location" {
  description = "Location in which to create resources"
  type        = string
}

variable "env" {
  description = "Unique identifier for the env, e.g. dev or ci or prod"
  type        = string
}

variable "base_name" {
  description = "Base name to use when naming resources"
  type        = string
}

variable "github_owner" {
  description = "GitHub owner used in Cloud Build trigger repository mapping"
  type        = string
}

variable "submission_url" {
  description = "Submission URL of the destination static-ct-api log"
  type        = string
}

variable "monitoring_url" {
  description = "Monitoring URL of the destination static-ct-api log"
  type        = string
}

variable "source_log_uri" {
  description = "URL of the source RFC6962 log"
  type        = string
}

variable "start_index_offset" {
  description = "Offset to apply to the start index"
  type        = number
}
