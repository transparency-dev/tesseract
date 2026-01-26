variable "project_id" {
  description = "GCP project ID where the log is hosted"
  type        = string
}

variable "base_name" {
  description = "Base name to use when naming resources"
  type        = string
}

variable "origin" {
  description = "Log origin"
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

variable "machine_type" {
  description = "GCP Compute Engine machine type to run the TesseraCT container on"
  type        = string
  default     = "n2-standard-4"
}

variable "spanner_pu" {
  description = "Amount of Spanner processing units"
  type        = number
  default     = 100
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

variable "enable_publication_awaiter" {
  description = "If true, waits for certificates to be integrated into the log before returning an SCT."
  type        = bool
  default     = true
}

variable "create_internal_load_balancer" {
  description = "If true, sets up an internal load balancer in front of TesseraCT servers."
  type        = bool
  default     = false
}

variable "public_bucket" {
  description = "Set to true to make the log's GCS bucket publicly accessible."
  type        = bool
  default     = false
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

variable "log_public_key_secret_name" {
  description = "Secret manager secret version resource for the log public key. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}."
  type        = string
}

variable "log_private_key_secret_name" {
  description = "Secret manager secret version resource for the log private key. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}."
  type        = string
  default     = "-secret"
}

variable "additional_signer_private_key_secret_names" {
  description = "List of additional private key secret names for checkpoint secondary signers. Format: projects/{projectId}/secrets/{secretName}/versions/{secretVersion}. These secrets MUST be formatted as serialised note signers."
  type        = list(string)
}

variable "gce_health_checks" {
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
