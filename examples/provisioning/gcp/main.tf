# Stands up DSS on Compute Engine. Layer one only: configuring what is inside
# the instance is a second apply, because Terraform resolves provider
# configuration during planning. See ../README.md.

terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

module "dss" {
  source = "../../../modules/dss-gcp"

  project_id          = var.project_id
  region              = var.region
  zone                = var.zone
  allowed_cidr_blocks = var.allowed_cidr_blocks
  license_json        = var.license_json
}

output "dss_url" {
  description = "Where DSS answers once it has finished installing."
  value       = module.dss.dss_url
}
