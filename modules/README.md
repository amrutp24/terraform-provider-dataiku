# Modules

One module lives here. The ones that stand up an instance live in their own
repositories, because the Terraform Registry derives a module's address from its
repository name — `terraform-<PROVIDER>-<NAME>` — and this repository is a
provider.

## In this repository

| Module | Creates | Provider the caller configures |
| --- | --- | --- |
| [`dss-platform`](dss-platform) | Groups, code environments, projects, permissions | `dataiku` |

## Published separately

| Module | Repository | Creates |
| --- | --- | --- |
| `amrutp24/dss/aws` | [terraform-aws-dss](https://github.com/amrutp24/terraform-aws-dss) | Security group, EC2 instance |
| `amrutp24/dss/google` | [terraform-google-dss](https://github.com/amrutp24/terraform-google-dss) | Firewall rules, Compute Engine instance |
| `amrutp24/dss/azurerm` | [terraform-azurerm-dss](https://github.com/amrutp24/terraform-azurerm-dss) | Resource group, network, NSG, Linux VM |
| `amrutp24/dss-bootstrap/null` | [terraform-null-dss-bootstrap](https://github.com/amrutp24/terraform-null-dss-bootstrap) | Nothing — renders the install script the three share |

They used to be copied in here as well. Two copies of a module are two things to
keep correct, and the second one is always the one nobody remembers to change.

## Why the two layers cannot be one apply

Terraform resolves **provider configuration during planning**, before any
resource exists. The `dataiku` provider needs an instance address and an API
key, so a configuration that creates the instance and then configures it has to
know both before creating anything. It deadlocks.

No amount of module structure removes this. Modules make the halves reusable;
they do not make them simultaneous.

In practice that means two root configurations with separate state. Layer two
reads layer one's outputs — through a remote state data source, a variable, or
whatever your setup prefers:

```hcl
# layer one: the instance
module "dss" {
  source              = "amrutp24/dss/aws"
  version             = "~> 0.1"
  allowed_cidr_blocks = ["203.0.113.0/24"]
}
```

```hcl
# layer two: what is inside it, applied once the instance answers
provider "dataiku" {
  host = var.dss_url # from layer one's output
}

module "platform" {
  source = "github.com/amrutp24/terraform-provider-dataiku//modules/dss-platform"
  groups = { data_scientists = { description = "Analysts" } }
}
```

Splitting them is also just better: you can rebuild an instance without touching
its configuration, and change configuration without risking the instance.

## Getting the API key across

Layer two needs an API key, and a fresh instance has no way to produce one
without a browser. The bootstrap runs `dsscli api-key-create` and writes the
result to `/var/lib/dataiku-terraform-key.json`, mode 0600.

Moving it from there is the one genuinely platform-specific step — a cloud
secret manager is cleanest, since nothing sensitive passes through Terraform
state. Each cloud module's README covers the options for its platform.

## Working examples

[`examples/provisioning`](../examples/provisioning) calls each layer-one module;
[`examples/platform`](../examples/platform) calls `dss-platform`.
