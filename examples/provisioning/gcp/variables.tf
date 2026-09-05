variable "project_id" {
  description = "GCP project to create everything in."
  type        = string
}

variable "region" {
  description = "GCP region."
  type        = string
  default     = "europe-west1"
}

variable "zone" {
  description = "Zone within the region."
  type        = string
  default     = "europe-west1-b"
}

variable "allowed_cidr_blocks" {
  description = "Who may reach the DSS port. No default on purpose."
  type        = list(string)
}

variable "license_json" {
  description = "Contents of a DSS licence file, applied at install time."
  type        = string
  default     = ""
  sensitive   = true
}
