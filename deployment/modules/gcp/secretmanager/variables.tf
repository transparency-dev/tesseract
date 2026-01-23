variable "base_name" {
  description = "Base name to use when naming resources"
  type        = string
}

variable "ephemeral" {
  description = "Set to true if this is a throwaway/temporary log instance. Will set attributes on created resources to allow them to be disabled/deleted more easily."
  default     = false
  type        = bool
}

