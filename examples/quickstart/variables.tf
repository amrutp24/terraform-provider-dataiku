variable "owner" {
  description = "Login of the DSS user who owns the project. A fresh instance has exactly one: admin."
  type        = string
  default     = "admin"
}
