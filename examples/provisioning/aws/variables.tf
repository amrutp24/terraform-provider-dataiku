variable "region" {
  description = "AWS region."
  type        = string
  default     = "eu-west-1"
}

variable "allowed_cidr_blocks" {
  description = "Who may reach the DSS port. No default on purpose."
  type        = list(string)
}

variable "key_name" {
  description = "Existing EC2 key pair for SSH. Null if you do not need a shell."
  type        = string
  default     = null
}

variable "license_json" {
  description = "Contents of a DSS licence file, applied at install time."
  type        = string
  default     = ""
  sensitive   = true
}
