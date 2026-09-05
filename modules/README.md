# Modules

Four modules, in two layers.

```
                    layer one: an instance exists
  ┌───────────────┐  ┌───────────────┐  ┌─────────────────┐
  │   dss-aws     │  │    dss-gcp    │  │    dss-azure    │
  └───────┬───────┘  └───────┬───────┘  └────────┬────────┘
          └──────────────────┼───────────────────┘
                     ┌───────▼────────┐
                     │ dss-bootstrap  │  renders the install script
                     └────────────────┘

                    ─── separate apply ───

                    layer two: what is inside it
                     ┌────────────────┐
                     │  dss-platform  │  groups, code envs, projects
                     └────────────────┘
```

| Module | Creates | Provider the caller configures |
| --- | --- | --- |
| [`dss-aws`](dss-aws) | Security group, EC2 instance | `aws` |
| [`dss-gcp`](dss-gcp) | Firewall rules, Compute Engine instance | `google` |
| [`dss-azure`](dss-azure) | Resource group, network, NSG, Linux VM | `azurerm` |
| [`dss-bootstrap`](dss-bootstrap) | Nothing — renders the install script | none |
| [`dss-platform`](dss-platform) | Groups, code environments, projects, permissions | `dataiku` |

None of them declares a `provider` block. That is deliberate: a module with its
own provider configuration cannot be used twice against different targets, and
Terraform warns about it. The caller configures the provider and the module
inherits it.

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
# layer one
module "dss" {
  source              = "github.com/amrutp24/terraform-provider-dataiku//modules/dss-aws"
  region              = "eu-west-1"
  allowed_cidr_blocks = ["203.0.113.0/24"]
}
```

```hcl
# layer two, applied once the instance answers
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
state. [`dss-bootstrap`](dss-bootstrap) covers the options.

## Working examples

[`examples/provisioning`](../examples/provisioning) calls each layer-one module;
[`examples/platform`](../examples/platform) calls `dss-platform`.
