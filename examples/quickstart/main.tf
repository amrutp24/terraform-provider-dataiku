# The smallest configuration that leaves something to look at: a group, a
# project the group can work in, and a variable on the project.
#
# dev/quickstart.sh applies this against a DSS it has just started in Docker.
# It works against any other instance too: set DATAIKU_HOST and
# DATAIKU_API_KEY and run terraform apply here.
#
# Nothing in it needs a licence.

terraform {
  required_version = ">= 1.8"

  required_providers {
    dataiku = {
      source  = "amrutp24/dataiku"
      version = "~> 0.8"
    }
  }
}

# host and api_key come from DATAIKU_HOST and DATAIKU_API_KEY.
provider "dataiku" {}

resource "dataiku_group" "analysts" {
  name        = "quickstart_analysts"
  description = "Created by the Terraform quickstart"

  permissions = {
    mayCreateProjects = true
  }
}

resource "dataiku_project" "quickstart" {
  project_key = "QUICKSTART"
  name        = "Terraform quickstart"
  owner       = var.owner
  short_desc  = "Made by terraform apply, not by hand"
  tags        = ["managed-by-terraform"]
}

resource "dataiku_project_variables" "quickstart" {
  project_key = dataiku_project.quickstart.project_key
  standard    = jsonencode({ environment = "local" })
}

# Authoritative: these are all the group grants the project has.
resource "dataiku_project_permissions" "quickstart" {
  project_key = dataiku_project.quickstart.project_key

  permission {
    group                 = dataiku_group.analysts.name
    read_project_content  = true
    write_project_content = true
    read_dashboards       = true
    run_scenarios         = true
  }
}

output "project_key" {
  description = "Open <your instance>/projects/<this>/ to see it."
  value       = dataiku_project.quickstart.project_key
}
