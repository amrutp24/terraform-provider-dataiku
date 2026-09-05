# Security policy

## Reporting a vulnerability

Please report security issues privately through GitHub's
[private vulnerability reporting](https://github.com/amrutp24/terraform-provider-dataiku/security/advisories/new)
rather than opening a public issue.

Include what the issue lets an attacker do, the provider and DSS versions, and a
way to reproduce it.

A vulnerability in Dataiku DSS itself belongs with
[Dataiku](https://www.dataiku.com/), not here. This repository covers only the
Terraform provider.

## What ends up in Terraform state

Terraform state is not encrypted, and this provider stores things in it that are
worth protecting:

- `dataiku_user.password`
- `dataiku_connection.params_json`, which usually carries database credentials
- `dataiku_project_variables.local`, commonly used for per-environment secrets

These are marked sensitive, so Terraform keeps them out of plan and apply output,
but **being sensitive does not keep them out of state**. Treat the state file as
a secret: use a backend that encrypts at rest and restricts who can read it.

The API key itself is never written to state. Supply it through the
`DATAIKU_API_KEY` environment variable rather than in configuration.

## Scope

The provider sends what it is configured with to the DSS instance named by
`host`, and makes no other network calls.
