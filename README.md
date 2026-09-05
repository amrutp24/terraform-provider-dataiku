# Terraform Provider for Dataiku DSS

Manage Dataiku DSS projects, users, groups, connections and permissions as code,
through the [DSS public REST API](https://doc.dataiku.com/dss/latest/publicapi/rest.html).

> **Scope.** This provider manages objects *inside* a running DSS instance. It does
> not provision the instance itself — for that, use the cloud provider of your
> choice or Dataiku Fleet Manager.

## Requirements

- Terraform 1.8 or later, or 1.11 for the write-only `_wo` arguments that keep
  secrets out of state
- A Dataiku DSS instance, and an API key for it. DSS must be licensed for the
  public REST API — the Free Edition is not, on its own.

## Contents

| Resources | Data sources |
| --- | --- |
| `dataiku_project` | `dataiku_project` |
| `dataiku_project_permissions` | `dataiku_projects` |
| `dataiku_project_variables` | `dataiku_user` |
| `dataiku_user` | `dataiku_group` |
| `dataiku_group` | `dataiku_connection` |
| `dataiku_connection` | `dataiku_project_folder` |
| `dataiku_code_env` | |
| `dataiku_project_folder` | |
| `dataiku_scenario` | |

Every resource supports `terraform import`.

## Usage

```hcl
terraform {
  required_providers {
    dataiku = {
      source  = "amrutp24/dataiku"
      version = "~> 0.1"
    }
  }
}

provider "dataiku" {
  host = "https://dss.example.com"
  # api_key comes from the DATAIKU_API_KEY environment variable
}

resource "dataiku_group" "data_scientists" {
  name        = "data_scientists"
  description = "Analysts who build and run models"
}

resource "dataiku_project" "analytics" {
  project_key = "ANALYTICS"
  name        = "Analytics"
  owner       = "admin"
  tags        = ["managed-by-terraform"]
}

resource "dataiku_project_permissions" "analytics" {
  project_key = dataiku_project.analytics.project_key

  permission {
    group                 = dataiku_group.data_scientists.name
    read_project_content  = true
    write_project_content = true
    run_scenario          = true
  }
}
```

## Provisioning the instance itself

This provider configures a DSS instance that already exists. Creating the
instance is a separate layer, and a separate tool: Terraform providers do not
provision generic compute.

[`modules/dss-bootstrap`](modules/dss-bootstrap) covers that half. It renders
the DSS install script and creates nothing itself, so the same output works as
EC2 user-data, a GCE startup script, Azure custom-data, a `remote-exec` payload
against a host you already own, or a Packer provisioner. Nothing in it is tied
to a cloud.

[`examples/provisioning`](examples/provisioning) has four root configurations
you can apply directly — Docker, AWS, GCP and Azure. Each stands up a running
DSS; the Docker one also shows the configuration layer on top.

Note that the two layers need **separate applies**: Terraform builds a
provider's configuration during planning, so the instance has to exist and have
an API key before the plan that configures it.

## Authentication

The provider authenticates with a DSS API key, sent as the HTTP Basic username
with an empty password — the same scheme the official Dataiku clients use.

| Setting | Argument | Environment variable |
| --- | --- | --- |
| Instance URL | `host` | `DATAIKU_HOST` |
| API key | `api_key` | `DATAIKU_API_KEY` |
| Skip TLS verification | `insecure` | `DATAIKU_INSECURE` |

Prefer the environment variable for the key so it never lands in configuration.
Create a key under **Administration → Security → Global API keys** in DSS. Most
resources here need a key with admin rights; `dataiku_project` and the project
sub-resources work with a key that only has rights on the projects involved.

## Design notes

Some behaviour is worth knowing before you adopt this:

**Updates preserve fields the provider does not model.** DSS replaces a whole
document on `PUT`, so every update reads the current document, overlays only the
managed fields, and writes it back. A DSS upgrade that adds new settings will not
have them silently reset by an apply.

**Group abilities are passed through by their raw API name.** The set of
`may*` ability flags differs between DSS versions and editions and is not part of
the published REST reference, so `dataiku_group` takes a `permissions` map keyed
by the raw field name rather than inventing a fixed list of attributes that might
silently do nothing. Discover the names your instance supports with the
`dataiku_group` data source:

```hcl
data "dataiku_group" "reference" {
  name = "administrators"
}

output "abilities" {
  value = data.dataiku_group.reference.definition_json
}
```

Abilities you leave out of the map keep whatever value the instance already has.

**Connection parameters are not refreshed.** DSS redacts secrets when reading a
connection back, so writing the redacted document again would destroy the stored
credentials. `params_json` is therefore written but never refreshed once set, and
changes made in the DSS interface do not show as drift. A `terraform import`
reads the redacted parameters in once so you have a starting point to edit.

**Two resources are authoritative.** `dataiku_project_permissions` replaces every
group grant on a project and `dataiku_project_variables` replaces the project's
whole variable document. Anything added through the DSS interface is removed on
the next apply. Use at most one of each per project.

**`project_folder_id` is create-only.** DSS does not report a project's folder
through the public API, so the value is applied at creation, never refreshed, and
changing it forces a new project. To see which projects a folder holds, read the
`dataiku_project_folder` data source.

**A folder's contents are on the data source, not the resource.** Projects and
child folders point at a folder rather than the other way round, so a resource
attribute would always be read before the things that populate it exist. The
`dataiku_project_folder` resource therefore exposes only what the folder itself
owns, and the matching data source exposes what is in it.

## Development

Requires Go 1.25.8 or later, which is what the Terraform plugin libraries pin.

```bash
make build   # compile the provider
make test    # unit tests
make lint    # gofmt + go vet
make docs    # regenerate docs/ from the schemas and examples/
```

To try local changes, add a dev override to `~/.terraformrc` (or
`%APPDATA%\terraform.rc` on Windows) and skip `terraform init`:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/amrutp24/dataiku" = "/path/to/this/repo"
  }
  direct {}
}
```

### A local DSS instance

`dev/` brings up a disposable DSS in Docker to develop against:

```bash
docker compose -f dev/docker-compose.yml up -d
```

See [dev/README.md](dev/README.md) for first-run setup and how to mint an API key.

### Acceptance tests

The acceptance tests run against an in-process fake DSS by default, so
`go test ./...` needs no instance. Setting `DATAIKU_HOST` and `DATAIKU_API_KEY`
points them at a real one instead. Either way they create and delete real
objects, so use a scratch instance, never production:

```bash
export DATAIKU_HOST=https://dss-scratch.example.com
export DATAIKU_API_KEY=...
make testacc
```

They are skipped unless `TF_ACC=1` is set, which `make testacc` does for you.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Mozilla Public License 2.0. See [LICENSE](LICENSE).
