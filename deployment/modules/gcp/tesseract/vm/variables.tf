variable "project_id" {
  description = "GCP project ID where the log is hosted"
  type        = string
}

variable "base_name" {
  description = "Base name to use when naming resources"
  type        = string
}

variable "origin_suffix" {
  description = "Origin suffix, appended to base_name"
  type        = string
}

variable "not_after_start" {
  description = "Start of the range of acceptable NotAfter values, inclusive. Leaving this empty implies no lower bound to the range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z."
  default     = ""
  type        = string
}

variable "not_after_limit" {
  description = "Cut off point of notAfter dates - only notAfter dates strictly *before* notAfterLimit will be accepted. Leaving this empty means no upper bound on the accepted range. RFC3339 UTC format, e.g: 2024-01-02T15:04:05Z."
  default     = ""
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

variable "docker_env" {
  description = "Unique identifier for the Docker env, e.g. dev or ci or prod"
  type        = string
}

variable "server_docker_image" {
  description = "The full image URL (path & tag) for the Docker image to deploy in Cloud Run"
  type        = string
}

variable "spanner_pu" {
  description = "Amount of Spanner processing units"
  type = number
  default = 100
}

variable "batch_max_size" {
  description = "Maximum number of entries to process in a single sequencing batch."
  type = number
  default = 1024
}

variable "batch_max_age" {
  description = "Maximum age of entries in a single sequencing batch."
  type = string
  default = "500ms"
}

variable "ephemeral" {
  description = "Set to true if this is a throwaway/temporary log instance. Will set attributes on created resources to allow them to be disabled/deleted more easily."
  default     = false
  type        = bool
}

variable "trace_fraction" {
  description = "Fraction of open-telemetry span traces to sample."
  default     = 0
  type        = number
}
