variable "dss_version" {
  description = "Tag of the dataiku/dss image to run."
  type        = string
  default     = "latest"
}

variable "dss_port" {
  description = "Port on the host that DSS is published on."
  type        = number
  default     = 10000
}

variable "build_code_envs" {
  description = "Whether Terraform asks DSS to install code environment packages. Needs outbound network access from the container."
  type        = bool
  default     = false
}
