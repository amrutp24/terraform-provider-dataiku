# Terraform Provider for Dataiku DSS

Manage Dataiku DSS projects, users, groups, connections and permissions as code,
through the [DSS public REST API](https://doc.dataiku.com/dss/latest/publicapi/rest.html).

> **Scope.** This provider manages objects *inside* a running DSS instance. It does
> not provision the instance itself — for that, use the cloud provider of your
> choice or Dataiku Fleet Manager.

## Contents

| Resources | Data sources |
| --- | --- |
| `dataiku_project` | `dataiku_project` |
| `dataiku_project_permissions` | `dataiku_projects` |
| `dataiku_project_variables` | `dataiku_user` |
| `dataiku_user` | `dataiku_group` |
| `dataiku_group` | `dataiku_connection` |
| `dataiku_connection` | |

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
changing it forces a new project.

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

### Acceptance tests

Acceptance tests create and delete real objects. Point them at a scratch
instance, never production:

```bash
export DATAIKU_HOST=https://dss-scratch.example.com
export DATAIKU_API_KEY=...
make testacc
```

They are skipped unless `TF_ACC=1` is set, which `make testacc` does for you.

## Publishing to the Terraform Registry

The repository is already set up for the registry: `.goreleaser.yml`,
`.github/workflows/release.yml` and `terraform-registry-manifest.json` follow
HashiCorp's scaffolding, and the manifest declares protocol `6.0` because this is
a Plugin Framework provider.

The provider address is `registry.terraform.io/amrutp24/dataiku`, set in the Go
module path and in `main.go`. If you publish under a different account or an
organization, change it in both places and re-run `go mod tidy && make docs`.

1. Push the code to a **public** GitHub repository named
   `terraform-provider-dataiku`. The name matters: the registry derives the
   provider name from it.

   ```bash
   gh repo create amrutp24/terraform-provider-dataiku --public --source=. --remote=origin --push
   ```
2. Generate an RSA or DSA GPG key (ECC is not supported) and add the
   ASCII-armored public key at
   [registry.terraform.io/settings/gpg-keys](https://registry.terraform.io/settings/gpg-keys).
3. Add the private key and its passphrase as the repository secrets
   `GPG_PRIVATE_KEY` and `PASSPHRASE`.
4. Tag a semver release and push it — the release workflow builds every
   OS/arch, signs the checksums, and attaches the assets:

   ```bash
   git tag v0.1.0 && git push origin v0.1.0
   ```

5. Sign in at [registry.terraform.io](https://registry.terraform.io) with the
   GitHub account that has admin on the repository, then go to
   **Publish → Provider** and select it. The registry installs a webhook, so
   later tags publish on their own.

Preview how `docs/` will render with the
[registry doc preview tool](https://registry.terraform.io/tools/doc-preview).

Until it is published, you can consume the provider from a local filesystem
mirror or the dev override shown above.

## License

Mozilla Public License 2.0. See [LICENSE](LICENSE).
