variable "location" {
  description = "Azure region."
  type        = string
  default     = "westeurope"
}

variable "allowed_cidr_blocks" {
  description = "Who may reach the DSS port. No default on purpose."
  type        = list(string)
}

variable "ssh_public_key" {
  description = "SSH public key for the admin user. Password authentication is disabled."
  type        = string
}

variable "license_json" {
  description = "Contents of a DSS licence file, applied at install time."
  type        = string
  default     = ""
  sensitive   = true
}
