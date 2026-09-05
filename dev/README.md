# Local DSS instance for development

A disposable Dataiku DSS instance to exercise the provider against something
real, before pointing it at anything you care about.

Self-hosted DSS is Linux-only, so on Windows or macOS this runs through Docker.
The image is Dataiku's own evaluation image (~4 GB) and is not a production
setup.

## Start it

```bash
docker compose -f dev/docker-compose.yml up -d
```

The first start takes several minutes while DSS initialises its data directory.
Watch it come up with:

```bash
docker compose -f dev/docker-compose.yml logs -f
```

Then open <http://localhost:10000> and sign in as **admin / admin**.

## You need a licence that includes the public API

**The Free / Community Edition does not license the public REST API.** On first
launch DSS asks you to register, which installs a `COMMUNITY` licence, and the
API key screen then reports:

> DSS API is not available with your Free Edition license

This provider talks to nothing but that API, so a Community-licensed instance
cannot be used to develop or test against. To use a local instance you need a
licence that includes the API — a Dataiku
[trial](https://www.dataiku.com/product/get-started), an academic licence, or a
commercial one. Install it with:

```bash
docker exec dataiku-dss-dev /home/dataiku/dss/bin/dsscli set-license --file /path/inside/container/license.json
```

If you have no such licence, skip the container entirely: the test suite runs
against an in-process fake DSS and needs no instance at all.

## Get an API key

With a licence that includes the API, as the `admin` user:

**Administration → Security → Global API keys → New API key**

Give it admin rights, then copy the secret — DSS shows it once.

A **personal** API key works too and is created by any user under **Profile &
settings → API keys**. A personal key carries exactly that user's rights, so it
must belong to an admin for the `dataiku_user`, `dataiku_group` and
`dataiku_connection` resources to work. The project resources only need rights
on the projects involved.

## Point the provider at it

```bash
export DATAIKU_HOST=http://localhost:10000
export DATAIKU_API_KEY=<the key you just copied>
```

Then either drive it with Terraform through a dev override (see the root
README) or run the acceptance tests against it:

```bash
make testacc
```

> The acceptance tests create and delete real projects, users, groups and
> connections. Only ever run them against this throwaway instance.

## Reset or remove it

```bash
docker compose -f dev/docker-compose.yml down          # stop, keep data
docker compose -f dev/docker-compose.yml down -v       # stop and wipe data
```

## Pinning a version

`latest` tracks the newest DSS release. To test against a specific one:

```bash
DSS_VERSION=14.7.0 docker compose -f dev/docker-compose.yml up -d
```
