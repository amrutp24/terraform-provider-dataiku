---
page_title: "Getting started"
subcategory: ""
description: |-
  From nothing to a first apply: a throwaway DSS in Docker with one command, or an instance you already have.
---

# Getting started

This provider manages what is inside a running Dataiku DSS instance: projects,
groups, users, connections, code environments, scenarios, API keys. It does
not create the instance. There are two ways to get to a first apply.

## Without an instance of your own

With Docker and Terraform installed, one command starts a disposable DSS,
makes an API key and applies a small example:

```shell
git clone https://github.com/amrutp24/terraform-provider-dataiku
cd terraform-provider-dataiku
dev/quickstart.sh
```

On Windows without bash, run `dev\quickstart.ps1` instead.

When it finishes, DSS is at `http://localhost:10000` (sign in as `admin` /
`admin`) and the project Terraform made is at
`http://localhost:10000/projects/QUICKSTART/`. The first time you open DSS it
asks you to register for the Free Edition or enter a licence. Nothing in the
example needs either.

What was applied is `examples/quickstart` in the repository. Change it and
apply again, or use the key the script saved to point your own configuration
at the instance:

```shell
source dev/.quickstart.env
terraform -chdir=examples/quickstart plan
```

## Against an instance you already have

You need the instance's URL and an API key. Create a key in DSS under
**Administration > Security > Global API keys**. Most resources need a key
with admin rights; projects and their sub-resources work with a key that only
has rights on the projects involved.

First check that the instance serves the public API to that key. This should
print `200`:

```shell
curl -su "$DATAIKU_API_KEY:" "$DATAIKU_HOST/public/api/admin/general-settings/" -o /dev/null -w '%{http_code}'
```

Then a first configuration:

```terraform
terraform {
  required_providers {
    dataiku = {
      source  = "amrutp24/dataiku"
      version = "~> 0.9"
    }
  }
}

# host and api_key are read from DATAIKU_HOST and DATAIKU_API_KEY.
provider "dataiku" {}

resource "dataiku_project" "sandbox" {
  project_key = "TF_SANDBOX"
  name        = "Terraform sandbox"
  owner       = "admin"
}
```

```shell
export DATAIKU_HOST=https://dss.example.com
export DATAIKU_API_KEY=...
terraform init
terraform plan
```

Keep the key in the environment variable so it never lands in configuration.

If the instance already has projects, users and groups you want Terraform to
manage, import them before you change anything. The guide on adopting an
existing instance covers that.

## What needs a licence

The provider works against a DSS 15 instance with no licence installed. What
a licence changes is which features DSS will run:

- A `dataiku_scenario` with a `temporal` trigger, or with custom Python steps,
  is refused by an unlicensed instance.
- The containerized execution resources store their settings on any instance,
  but running workloads in containers needs a licence that includes it.
- `dataiku_user` needs a `user_profile` the instance defines. An unlicensed
  instance defines only `NONE`, and the provider refuses anything else there
  at plan time, because DSS would accept it and then break.

## Tested versions

The acceptance suite is run against DSS 15.2.0 and 15.0.0. Nothing older than
DSS 15 has been tried.
