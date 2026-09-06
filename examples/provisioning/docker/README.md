# Provisioning and configuring DSS in one place

A worked example of the two layers, using Docker so it needs no cloud account.
The platform is incidental: replace `docker_container.dss` with a GCE instance,
an EC2 instance, or a host you already own, and everything under *layer 2* is
unchanged.

For a VM rather than a container, use one of the published cloud modules —
[aws](https://github.com/amrutp24/terraform-aws-dss),
[google](https://github.com/amrutp24/terraform-google-dss),
[azurerm](https://github.com/amrutp24/terraform-azurerm-dss) — or
[`amrutp24/dss-bootstrap/null`](https://github.com/amrutp24/terraform-null-dss-bootstrap)
directly if your target is not among them.

## Why two applies

Terraform builds a provider's configuration during planning, so the `dataiku`
provider needs a reachable instance and a working API key *before* the plan that
uses it. That cannot be satisfied in the same apply that creates the instance.

```bash
terraform apply -target=docker_container.dss
```

Then mint an API key on the instance and export it:

```bash
export DATAIKU_API_KEY=$(docker exec dataiku-dss /home/dataiku/dss/bin/dsscli api-key-create --label terraform --admin true --output json | python -c 'import json,sys; print(json.load(sys.stdin)[0]["key"])')
```

Then apply the rest:

```bash
terraform apply
```

In a real setup, split the two layers into separate root modules with their own
state and have the second read the first's outputs. That is less awkward than
`-target`, and it lets you rebuild the instance without touching its
configuration.

## First run

DSS takes several minutes to initialise on first start. Watch it with
`docker logs -f dataiku-dss`, and wait for the API to answer before minting the
key — a `401` means the backend is up, while `502` means it is still starting.

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:10000/public/api/auth/info
```

## Licence

The provider talks to the DSS public REST API, which the Free Edition does not
license on its own — though the Enterprise trial bundled with it does, while the
trial lasts. See [dev/README.md](../../../dev/README.md).

## Cleaning up

```bash
terraform destroy
```

The named volume holds the data directory, so destroying the container without
also removing the volume keeps every project.
