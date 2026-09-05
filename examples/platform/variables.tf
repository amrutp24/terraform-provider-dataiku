variable "dss_url" {
  description = "Base URL of the DSS instance, for example the dss_url output of a provisioning module."
  type        = string
}

variable "owner" {
  description = "DSS login that owns the projects created here."
  type        = string
  default     = "admin"
}
