variable "folder_name" {
  description = <<-EOT
    Project folder to create and place the projects in. Null leaves them at the
    root of the hierarchy.
  EOT
  type        = string
  default     = null
}

variable "groups" {
  description = <<-EOT
    Groups to create, keyed by name.

    `permissions` holds DSS ability flags under their raw API names, because
    the set differs between DSS versions and editions. Read an existing group
    with the `dataiku_group` data source to see what yours accepts. Abilities
    left out keep whatever the instance already has.
  EOT
  type = map(object({
    description = optional(string)
    admin       = optional(bool, false)
    permissions = optional(map(bool), {})
  }))
  default = {}
}

variable "code_envs" {
  description = <<-EOT
    Code environments to create, keyed by name. `packages` is
    requirements.txt content: one specification per line.
  EOT
  type = map(object({
    lang                    = optional(string, "PYTHON")
    python_interpreter      = optional(string)
    packages                = optional(string)
    install_jupyter_support = optional(bool, true)
  }))
  default = {}

  validation {
    condition     = alltrue([for env in var.code_envs : contains(["PYTHON", "R"], env.lang)])
    error_message = "Each code environment's lang must be PYTHON or R."
  }
}

variable "build_code_envs" {
  description = <<-EOT
    Whether an apply waits for DSS to resolve and install each environment's
    packages. That runs pip or conda on the instance and needs outbound access
    to a package index, so it is off by default: the environments are defined
    and you build them when you are ready.
  EOT
  type        = bool
  default     = false
}

variable "projects" {
  description = <<-EOT
    Projects to create, keyed by project key. Keys may contain only letters,
    digits and underscores.

    `grants` is authoritative for the project: it replaces every group grant on
    it. A project with an empty list is left alone rather than stripped.
  EOT
  type = map(object({
    name        = string
    owner       = string
    description = optional(string)
    short_desc  = optional(string)
    tags        = optional(list(string))
    grants = optional(list(object({
      group            = string
      read             = optional(bool, false)
      write            = optional(bool, false)
      read_dashboards  = optional(bool, false)
      write_dashboards = optional(bool, false)
      run_scenarios    = optional(bool, false)
      export_data      = optional(bool, false)
      admin            = optional(bool, false)
    })), [])
  }))
  default = {}

  validation {
    condition     = alltrue([for key in keys(var.projects) : can(regex("^[A-Za-z0-9_]+$", key))])
    error_message = "Project keys may contain only letters, digits and underscores."
  }
}
