# Everything a team needs inside a running DSS instance: who exists, what they
# may do, what they compute with, and where their work lives.
#
# This module declares no provider configuration. The caller configures the
# dataiku provider, which is what keeps this reusable across instances -- and
# is also why standing up an instance and configuring it cannot be one apply:
# Terraform resolves provider configuration during planning, before any
# instance exists.

terraform {
  required_version = ">= 1.5"

  required_providers {
    dataiku = {
      source  = "amrutp24/dataiku"
      version = ">= 0.2.0"
    }
  }
}

# Somewhere for the team's projects to live, so they are not loose at the root.
resource "dataiku_project_folder" "team" {
  count = var.folder_name == null ? 0 : 1

  name = var.folder_name
}

resource "dataiku_group" "this" {
  for_each = var.groups

  name        = each.key
  description = each.value.description
  admin       = each.value.admin
  permissions = each.value.permissions
}

resource "dataiku_code_env" "this" {
  for_each = var.code_envs

  name = each.key
  lang = each.value.lang

  python_interpreter      = each.value.python_interpreter
  packages                = each.value.packages
  install_jupyter_support = each.value.install_jupyter_support

  # Building runs pip or conda on the instance and can take minutes, so it is
  # the caller's choice whether an apply waits for it.
  install_packages_on_change = var.build_code_envs
}

resource "dataiku_project" "this" {
  for_each = var.projects

  project_key = each.key
  name        = each.value.name
  owner       = each.value.owner
  description = each.value.description
  short_desc  = each.value.short_desc
  tags        = each.value.tags

  project_folder_id = var.folder_name == null ? null : dataiku_project_folder.team[0].id
}

# Kept separate from the project because it is authoritative: it replaces every
# group grant, so a project with no entry here keeps whatever it already has
# rather than being silently stripped.
resource "dataiku_project_permissions" "this" {
  for_each = {
    for key, project in var.projects : key => project
    if length(project.grants) > 0
  }

  project_key = dataiku_project.this[each.key].project_key

  dynamic "permission" {
    for_each = each.value.grants
    content {
      group                 = permission.value.group
      read_project_content  = permission.value.read
      write_project_content = permission.value.write
      read_dashboards       = permission.value.read_dashboards
      write_dashboards      = permission.value.write_dashboards
      run_scenarios         = permission.value.run_scenarios
      export_datasets_data  = permission.value.export_data
      admin                 = permission.value.admin
    }
  }

  depends_on = [dataiku_group.this]
}
