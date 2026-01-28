variable "project_id" {
  description = "GCP project ID where the log is hosted"
  type        = string
}

variable "base_name" {
  description = "Base name to use when naming resources"
  type        = string
}

variable "bucket_name" {
  description = "Name of the GCS bucket. Defaults to '{var.project_id}-{var.base_name}-bucket if unspecicfied"
  type        = string
  default     = null
}

variable "location" {
  description = "Location in which to create resources"
  type        = string
}

variable "ephemeral" {
  description = "Set to true if this is a throwaway/temporary log instance. Will set attributes on created resources to allow them to be disabled/deleted more easily."
  type        = bool
}

variable "spanner_pu" {
  description = "Amount of Spanner processing units"
  type        = number
  default     = 100
}

variable "public_bucket" {
  description = "Set to true to make the log's GCS bucket publicly accessible"
  type        = bool
  default     = false
}
