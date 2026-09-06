# Stands up DSS on EC2. Layer one only: configuring what is inside the
# instance is a second apply, because Terraform resolves provider
# configuration during planning. See ../README.md.

terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = var.region
}

module "dss" {
  source  = "amrutp24/dss/aws"
  version = "~> 0.1"

  region              = var.region
  allowed_cidr_blocks = var.allowed_cidr_blocks
  key_name            = var.key_name
  license_json        = var.license_json
}

output "dss_url" {
  description = "Where DSS answers once it has finished installing."
  value       = module.dss.dss_url
}
