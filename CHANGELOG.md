# Changelog

All notable changes to this provider are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-09-06

No change to the provider itself. The binary is identical to 0.2.0; this
release covers the modules and examples that ship alongside it.

### Added

- `modules/dss-platform`, which configures a running instance: groups with
  their DSS ability flags, code environments, projects, and the permissions on
  them. Project grants live in their own resource because that resource is
  authoritative for a project, so a project listed without grants is left alone
  rather than stripped. Building code environments is off by default, since it
  runs pip on the instance and an apply should not hang on a package index by
  surprise.
- `examples/provisioning` for AWS, GCP and Azure, alongside the existing Docker
  one, and `examples/platform` for the configuration layer.

### Changed

- The modules that stand up an instance are published separately, because the
  Terraform Registry derives a module address from its repository name and this
  repository is a provider:

  | Was | Now |
  | --- | --- |
  | `modules/dss-aws` | [`amrutp24/dss/aws`](https://registry.terraform.io/modules/amrutp24/dss/aws) |
  | `modules/dss-gcp` | [`amrutp24/dss/google`](https://registry.terraform.io/modules/amrutp24/dss/google) |
  | `modules/dss-azure` | [`amrutp24/dss/azurerm`](https://registry.terraform.io/modules/amrutp24/dss/azurerm) |
  | `modules/dss-bootstrap` | [`amrutp24/dss-bootstrap/null`](https://registry.terraform.io/modules/amrutp24/dss-bootstrap/null) |

  The install script had been copied into each cloud module. It now has one
  home that the three depend on with a version constraint.

- The provisioning examples call those modules from the registry, so they show
  what a consumer writes rather than a relative path only this repository has.

## [0.2.0] - 2026-09-05

### Added

- Write-only arguments `password_wo` on `dataiku_user` and `params_json_wo` on
  `dataiku_connection`, each with a `_wo_version` marker to trigger rotation.
  These are never written to plan or state. They need Terraform 1.11 or later,
  and conflict with the persisted attributes they replace.
- `TestAccConnectionReachesDatabase`, which asks DSS to dial the database a
  connection describes rather than only checking that DSS accepted the
  document. It needs a reachable database, so it skips unless
  `DATAIKU_TEST_PG_HOST` is set; `dev/docker-compose.yml` provides one behind
  a `test` profile.

## [0.1.0] - 2026-09-05

### Added

- Initial provider, built on terraform-plugin-framework and speaking protocol 6.
- Resources: `dataiku_project`, `dataiku_project_permissions`,
  `dataiku_project_variables`, `dataiku_project_folder`, `dataiku_user`,
  `dataiku_group`, `dataiku_connection`, `dataiku_code_env`,
  `dataiku_scenario`. All support `terraform import`.
- Data sources: `dataiku_project`, `dataiku_projects`, `dataiku_project_folder`,
  `dataiku_user`, `dataiku_group`, `dataiku_connection`.
- `modules/dss-bootstrap`, which renders the DSS install script for any Linux
  target without declaring a provider of its own.
- Transient failures are retried with exponential backoff and jitter, honouring
  `Retry-After`. Configurable with `max_retries`.

### Notes

- Every resource was verified against a licensed DSS 15 instance, not only
  against the in-process fake used by the test suite.
- The provider talks exclusively to the DSS public REST API, which the Free
  Edition does not license on its own.
