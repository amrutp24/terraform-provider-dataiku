# Stands up DSS on an Azure Linux VM. Layer one only: configuring what is
# inside the instance is a second apply, because Terraform resolves provider
# configuration during planning. See ../README.md.

terraform {
  required_version = ">= 1.5"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 4.0"
    }
  }
}

provider "azurerm" {
  features {}
}

module "dss" {
  source = "../../../modules/dss-azure"

  location            = var.location
  allowed_cidr_blocks = var.allowed_cidr_blocks
  ssh_public_key      = var.ssh_public_key
  license_json        = var.license_json
}

output "dss_url" {
  description = "Where DSS answers once it has finished installing."
  value       = module.dss.dss_url
}
