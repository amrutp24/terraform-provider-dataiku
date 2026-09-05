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

Then open <http://localhost:10000> and sign in as **admin / admin**. On first
launch DSS asks you to fill in a registration form, which generates a Free
Edition licence automatically.

## Get an API key

The provider authenticates with a DSS API key. As the `admin` user:

**Administration → Security → Global API keys → New API key**

Give it admin rights, then copy the secret — DSS shows it once.

If your edition does not offer global API keys, a **personal** API key works
too and is created by any user under **Profile & settings → API keys**. A
personal key carries exactly that user's rights, so it must belong to an admin
for the `dataiku_user`, `dataiku_group` and `dataiku_connection` resources to
work. The project resources only need rights on the projects involved.

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
