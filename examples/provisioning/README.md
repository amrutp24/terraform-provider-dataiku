# Provisioning DSS

Four ways to stand up a Dataiku DSS instance with Terraform. Each directory is a
root configuration you can `terraform apply` directly, not a module to wire up.

| | What it creates | Needs |
| --- | --- | --- |
| [`docker/`](docker) | Image, volume, container on your own machine | Docker |
| [`aws/`](aws) | Security group and an EC2 instance | AWS account |
| [`gcp/`](gcp) | Firewall rules and a Compute Engine instance | GCP project with billing |
| [`azure/`](azure) | Resource group, network, NSG and a Linux VM | Azure subscription |

The three cloud configurations call modules published separately —
[`amrutp24/dss/aws`](https://github.com/amrutp24/terraform-aws-dss),
[`amrutp24/dss/google`](https://github.com/amrutp24/terraform-google-dss) and
[`amrutp24/dss/azurerm`](https://github.com/amrutp24/terraform-azurerm-dss).
They install DSS the same way, through
[`amrutp24/dss-bootstrap/null`](https://github.com/amrutp24/terraform-null-dss-bootstrap),
which renders the install script: one copy to keep correct rather than three
that drift.

## Two applies, not one

Terraform builds a provider's configuration during **planning**, so the
`dataiku` provider needs a reachable instance and a working API key *before* the
plan that uses it. An instance cannot be created and configured in the same
apply.

So each configuration here does layer one only: it produces a running DSS.
Configuring what is inside it — projects, groups, connections, code
environments — is a second root configuration using the `dataiku` provider, with
its own state. [`docker/`](docker) shows both halves together and explains the
handover.

Splitting them is not just a workaround. It lets you rebuild an instance without
touching its configuration, and change configuration without risking the
instance.

## Getting an API key out

Layer two needs an API key, and a brand-new instance has no way to produce one
without a browser. The bootstrap runs `dsscli api-key-create` and writes the
result to `/var/lib/dataiku-terraform-key.json`, mode 0600.

Retrieving it is the one genuinely platform-specific step:

- **A cloud secret manager** is cleanest — extend the script to push the key into
  Secrets Manager, Secret Manager or Key Vault, then read it back with that
  provider's data source. Nothing sensitive passes through Terraform state.
- **Over SSH**, with a `remote-exec` or an `external` data source.
- **By hand, once**: set `create_api_key = false` and create a global API key
  under Administration → Security after the instance is up.

## Before you apply

**`allowed_cidr_blocks` has no default, on purpose.** DSS holds your data and its
login page would otherwise be reachable from anywhere. Every configuration here
refuses `0.0.0.0/0` outright; edit the validation if you genuinely mean it.

**The first boot takes a while.** The installer downloads about two gigabytes and
then builds a Python environment. Several minutes is normal, and DSS will not
answer on its port until that finishes. Watch progress in the cloud console's
serial output, or `docker logs` for the local one.

**Size for memory.** DSS drops into a low-memory mode below roughly 16 GB and
says so in its logs. The defaults here — `m5.xlarge`, `n2-standard-4`,
`Standard_D4s_v5` — are about the smallest that behave normally, and they cost
real money while running. `terraform destroy` when you are done.

**The data directory is on the boot disk.** That keeps these configurations
short and readable, and it means replacing the instance loses everything. For
anything you care about, attach a separate persistent disk, mount it at
`data_dir`, and it will survive a rebuild.

## Licences

The provider talks to the DSS public REST API, which the Free Edition does not
licence on its own — though the Enterprise trial bundled with it does, while the
trial lasts. Pass a licence at install time with `license_json`, or register the
instance through its web interface on first visit. See
[`dev/README.md`](../../dev/README.md).
