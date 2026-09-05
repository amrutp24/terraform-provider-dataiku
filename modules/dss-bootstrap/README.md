# dss-bootstrap

Renders the script that installs Dataiku DSS on a Linux host.

This module deliberately creates **no infrastructure and declares no provider**.
It takes a DSS version and some settings and gives you back a shell script and a
cloud-init document. Where that script runs is your choice, which is what keeps
it usable on any cloud, on bare metal, or in an image build.

```
┌─────────────────┐   install_script   ┌──────────────────┐   dataiku provider   ┌──────────────┐
│ dss-bootstrap   │ ─────────────────► │ compute you own  │ ───────────────────► │ projects,    │
│ (this module)   │                    │ VM / host / AMI  │                      │ code envs, … │
└─────────────────┘                    └──────────────────┘                      └──────────────┘
```

## Usage

```hcl
module "dss" {
  source = "./modules/dss-bootstrap"

  dss_version  = "15.0.0"
  data_dir     = "/data/dataiku/dss_data"
  license_json = var.dss_license_json
}
```

Then hand `module.dss.install_script` to whatever runs it.

## Wiring it to a target

The script wants to run as root on a fresh Linux host with outbound HTTPS. Every
target below consumes the same output.

**Any cloud VM that takes cloud-init**

```hcl
user_data = module.dss.cloud_init
```

**GCE**

```hcl
resource "google_compute_instance" "dss" {
  # ...
  metadata_startup_script = module.dss.install_script
}
```

**EC2**

```hcl
resource "aws_instance" "dss" {
  # ...
  user_data = module.dss.install_script
}
```

**Azure**

```hcl
resource "azurerm_linux_virtual_machine" "dss" {
  # ...
  custom_data = base64encode(module.dss.cloud_init)
}
```

**A host you already have, over SSH**

```hcl
resource "terraform_data" "dss" {
  connection {
    host = var.dss_host
    user = var.ssh_user
  }

  provisioner "remote-exec" {
    inline = ["sudo bash -c '${module.dss.install_script}'"]
  }
}
```

**Baking an image with Packer** — write `install_script` to a file and use it as
a shell provisioner, so instances boot with DSS already installed instead of
downloading two gigabytes each time.

## Sizing

DSS wants roughly 16 GB of RAM to be comfortable; it drops into a low-memory
mode below that and says so in its logs. The data directory holds every project
and all configuration, so put it on a persistent disk you back up, separate from
the boot disk.

## Getting the API key out

The provider needs an API key, and on a brand-new instance the only place to
mint one without a browser is the host itself. With `create_api_key` set (the
default) the bootstrap runs `dsscli api-key-create` and writes the result to
`api_key_path` as JSON, mode 0600.

Retrieving it is the one genuinely platform-specific step, because it depends on
what your compute can talk to:

- **Cloud secret manager** — the cleanest option. Extend the script to push the
  key into Secret Manager / Secrets Manager / Key Vault, then read it back with
  that provider's data source. Nothing sensitive touches Terraform state along
  the way.
- **SSH** — fetch the file with a `remote-exec` or an `external` data source.
- **By hand, once** — set `create_api_key = false` and create a global API key
  in **Administration → Security → Global API keys** after the instance is up.

Whichever you pick, the key ends up in Terraform state if you read it into
Terraform, so treat that state as a secret.

## Two applies, not one

The instance has to exist and finish booting before the provider can configure
it, and Terraform builds its provider configuration during planning. Configuring
a DSS instance in the same apply that creates it does not work.

Either split the two layers into separate root modules with the second reading
the first's outputs, or apply in two passes with `-target`. Splitting is the
option that stays sane, and it also lets you rebuild the instance without
touching the configuration.

## Tests

```bash
terraform init -backend=false && terraform test
```

The module creates nothing, so the tests assert on the rendered script at plan
time: no credentials, no cloud, no cleanup. They cover the conditional blocks
(licence present or absent, API key wanted or not), that the guard against
reinstalling over a live data directory is present, that DSS is never run as
root, and that the variable validations actually fire.

## Licences

DSS needs a licence that includes the public REST API for the provider to be
able to do anything. The Free Edition does not include it on its own, though the
Enterprise trial that ships alongside it does, for as long as the trial runs.
See [../../dev/README.md](../../dev/README.md).
