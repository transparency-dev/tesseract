variable "project_id" {
  description = "GCP project ID where the log is hosted."
  type        = string
}

variable "log_names" {
  description = "Name of logs wired to the load balancer."
  type        = list(string)
}

variable "submission_host_suffix" {
  description = "Submission host suffix, appended to each logname."
  type        = string
}

variable "log_location" {
  description = "Region the logs are in."
  type        = string
}
