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

variable "location" {
  description = "Location in which to create resources"
  type        = string
}

variable "env" {
  description = "Unique identifier for the env, e.g. dev or ci or prod"
  type        = string
}

variable "server_docker_image" {
  description = "The full image URL (path & tag) for the Docker image to deploy in Cloud Run"
  type        = string
}

variable "machine_type" {
  description = "GCP Compute Engine machine type to run the TesseraCT container on"
  type        = string
  default     = "n2-standard-4"
}

variable "bucket" {
  description = "Log GCS bucket"
  type        = string
}

variable "log_spanner_instance" {
  description = "Log Spanner instance"
  type        = string
}

variable "log_spanner_db" {
  description = "Log Spanner database"
  type        = string
}

variable "antispam_spanner_db" {
  description = "Antispam Spanner database"
  type        = string
}

variable "additional_signer_private_key_secret_names" {
  description = "List of additional private key secret names for checkpoint secondary signers. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}. These secrets MUST be formatted in note signer format."
  type        = list(string)
}

variable "signer_public_key_secret_name" {
  description = "Public key secret name for checkpoints and SCTs signer. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}."
  type        = string
}

variable "signer_private_key_secret_name" {
  description = "Private key secret name for checkpoints and SCTs signer. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}."
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

variable "trace_fraction" {
  description = "Fraction of open-telemetry span traces to sample."
  default     = 0
  type        = number
}

variable "batch_max_size" {
  description = "Maximum number of entries to process in a single sequencing batch."
  type        = number
  default     = 1024
}

variable "batch_max_age" {
  description = "Maximum age of entries in a single sequencing batch."
  type        = string
  default     = "500ms"
}

variable "enable_publication_awaiter" {
  description = "If true, waits for certificates to be integrated into the log before returning an SCT."
  type        = bool
  default     = true
}

variable "rate_limit_old_not_before" {
  description = "Set to configure rate limiting for old submissions. See --rate_limit_old_not_before flag for format."
  type        = string
  default     = ""
}

variable "rate_limit_dedup" {
  description = "Set to rate limit duplicate submissions per second."
  type        = number
  default     = -1
}

variable "witness_policy" {
  description = "Set to apply a witness policy which will be used by TesseraCT to gather cosignatures for checkpoints."
  type        = string
  default     = ""
}

variable "health_checks" {
  description = "If true, enables GCE health checks."
  type        = bool
  default     = true
}

variable "accepted_roots" {
  description = "Path to the file containing root certificates that are acceptable to the log. Experimental, only accepts small files."
  type        = string
  default     = ""
}

variable "roots_remote_fetch_url" {
  description = "URL to fetch trusted roots from."
  type        = string
  default     = "https://ccadb.my.salesforce-sites.com/ccadb/RootCACertificatesIncludedByRSReportCSV"
}

variable "roots_remote_fetch_interval" {
  description = "Interval between two fetches from roots_fetch_url, e.g. \"1h\"."
  type        = string
  default     = "0s"
}
