# Layer two: what goes inside a DSS instance that already exists.
#
# Run this after the instance is up and you have an API key. It is a separate
# root configuration from the provisioning examples on purpose -- Terraform
# resolves provider configuration during planning, so an instance cannot be
# created and configured in one apply.

terraform {
  required_version = ">= 1.5"

  required_providers {
    dataiku = {
      source  = "amrutp24/dataiku"
      version = "~> 0.2"
    }
  }
}

provider "dataiku" {
  host = var.dss_url
  # api_key comes from DATAIKU_API_KEY
}

module "platform" {
  source = "../../modules/dss-platform"

  folder_name = "Analytics"

  groups = {
    data_scientists = {
      description = "Analysts who build and run models"
      permissions = {
        mayCreateProjects  = true
        mayWriteUnsafeCode = false
      }
    }
    readers_analytics = {
      description = "Dashboard-only access to the analytics projects"
    }
  }

  code_envs = {
    ml-python = {
      python_interpreter = "PYTHON311"
      packages           = <<-EOT
        scikit-learn==1.5.0
        pandas==2.2.2
      EOT
    }
  }

  projects = {
    ANALYTICS = {
      name       = "Analytics"
      owner      = var.owner
      short_desc = "Shared analytics workspace"
      tags       = ["managed-by-terraform"]

      grants = [
        {
          group           = "data_scientists"
          read            = true
          write           = true
          read_dashboards = true
          run_scenarios   = true
          export_data     = true
        },
        {
          group           = "readers_analytics"
          read_dashboards = true
        },
      ]
    }
  }
}

output "project_keys" {
  value = module.platform.project_keys
}

output "folder_id" {
  value = module.platform.folder_id
}
