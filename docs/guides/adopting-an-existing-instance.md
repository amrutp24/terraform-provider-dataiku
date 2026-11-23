---
page_title: "Adopting an existing instance"
subcategory: ""
description: |-
  Bring projects, groups, users and the rest of a running DSS instance under Terraform without changing them.
---

# Adopting an existing instance

Most instances were configured by hand long before Terraform arrives. Every
resource in this provider supports import, and importing is the safe way in:
it creates nothing and destroys nothing. You import what is there, run
`terraform plan`, and read the result before letting Terraform change
anything.

## Import with generated configuration

Terraform can write the configuration for you. List what you want to adopt in
`import` blocks:

```terraform
import {
  to = dataiku_project.analytics
  id = "ANALYTICS"
}

import {
  to = dataiku_project_permissions.analytics
  id = "ANALYTICS"
}

import {
  to = dataiku_group.data_scientists
  id = "data_scientists"
}

import {
  to = dataiku_user.jsmith
  id = "jsmith"
}
```

Then ask for a plan that also generates the resource blocks:

```shell
terraform plan -generate-config-out=generated.tf
```

The plan should read `N to import, 0 to add, 0 to change, 0 to destroy`.
Review `generated.tf`, move the blocks into your own files, and apply. The
apply records the objects in state and changes nothing on the instance.

Terraform still calls configuration generation experimental. With this
provider it has been checked for projects, their permissions and variables,
groups, users and API keys. For anything else, read what it generates with
more care, or write the block by hand and use the `terraform import` command
described at the end.

Each resource's page gives its import id. The ones that are not simply a name:

| Resource | Import id |
| --- | --- |
| `dataiku_project`, `dataiku_project_permissions`, `dataiku_project_variables` | The project key |
| `dataiku_code_env` | `<lang>/<name>`, for example `PYTHON/ml-python311` |
| `dataiku_scenario` | `<project_key>/<scenario_id>` |
| `dataiku_project_folder` | The id DSS assigned the folder |
| `dataiku_global_api_key` | The key's id, not its secret or label |

## What an import cannot recover

DSS does not give everything back, and a few attributes describe your intent
rather than the instance's state. After importing, check these.

**Secrets.** DSS never returns a user's password, a connection's secret
parameters or an API key's secret. An imported `dataiku_user` has a null
password, an imported `dataiku_connection` needs its credentials supplied
again in `params_json`, and an imported `dataiku_global_api_key` has no `key`.

**Abilities on groups and API keys.** The `permissions` map tracks only the
ability names your configuration lists, and an import has no configuration to
read. Generated configuration comes out as `permissions = {}`. Add the
abilities you want Terraform to manage. The `dataiku_group` data source shows
every ability the instance supports in `definition_json`.

**A user's groups.** Importing a user adopts its full group membership,
including the per-user group DSS creates on its own for users made through
the API. Keep every group in the generated list unless you mean to remove the
membership.

**A project's folder.** DSS does not report which folder a project is in, so
`project_folder_id` is null after an import. Leave it unset for an imported
project: setting it forces a new project.

**Project permissions are authoritative.** Once imported and applied,
`dataiku_project_permissions` owns every group grant on the project. A grant
somebody adds in the DSS interface later is removed by the next apply.

**Scenario ids made in the DSS interface.** A scenario created through the
API gets its id from its name, with anything that is not a letter, digit, dot
or dash replaced by an underscore. The DSS interface also uppercases, so a
scenario named `test1` in the browser has the id `TEST1`. Read the id from
the scenario's URL in DSS rather than working it out.

**Containerized execution settings.** `dataiku_container_image_build_config`
and `dataiku_container_execution_config` seed `settings_json` empty on import,
because the instance holds your settings merged with every default DSS added
and cannot say which were yours. Read `effective_json` and write
`settings_json` from the parts you mean to manage.
`dataiku_container_settings` records no previous values on import, so a later
destroy will say it cannot put them back rather than guess.

## One at a time

The `terraform import` command works too, for one object at a time against
configuration you have already written:

```shell
terraform import dataiku_group.data_scientists data_scientists
terraform plan
```

An empty plan afterwards means your configuration matches the instance.
