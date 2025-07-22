variable "project_id" {
  description = "GCP project ID where the log is hosted"
  type        = string
}

variable "log_names" {
  description = "Name of logs wired to the load balancer"
  type        = list(string)
}

variable "submission_host_suffix" {
  description = "submission host suffix, appended to each logname"
  type        = string
}
