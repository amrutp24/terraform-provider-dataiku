# Changelog

All notable changes to this provider are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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
