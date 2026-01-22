variable "base_name" {
  description = "Base name to use when naming resources."
  type        = string
}

variable "public_key_suffix" {
  description = "Suffix to apply to base_name to create the name of the public key."
  type        = string
  default     = "-public"
}

variable "private_key_suffix" {
  description = "Suffix to apply to base_name to create the name of the private key."
  type        = string
  default     = "-secret"
}
