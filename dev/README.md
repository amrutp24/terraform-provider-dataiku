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

This provider talks to nothing but the DSS public REST API, and **the Free
Edition does not licence it on its own**. The API key screen reports:

> DSS API is not available with your Free Edition license

A Free Edition licence does normally come with a time-limited **Enterprise
trial**, and the API works for as long as that trial is running. Check where
you stand:

```bash
curl -su "$DATAIKU_API_KEY:" http://localhost:10000/public/api/admin/licensing/status \
  | python -c "import json,sys; c=json.load(sys.stdin)['base']['licenseContent']; \
      print(c['licenseKind'], c['properties'].get('community.eeTrialUntil','no EE trial'))"
```

A `community.eeTrialUntil` date in the future means the API is available until
then. Once it passes, expect API access to stop, and you will need a Dataiku
[trial](https://www.dataiku.com/product/get-started), academic, or commercial
licence. Install one with:

```bash
docker exec dataiku-dss-dev /home/dataiku/dss/bin/dsscli set-license --file /path/inside/container/license.json
```

If you have no licence with API access, skip the container entirely: the test
suite runs against an in-process fake DSS and needs no instance at all.

## Licence profiles differ per instance

`dataiku_user.user_profile` only accepts profiles your licence grants. A trial
licences `DESIGNER` and `NONE` and nothing else, so `FULL_DESIGNER` or `READER`
will fail there. List what yours grants:

```bash
curl -su "$DATAIKU_API_KEY:" http://localhost:10000/public/api/admin/licensing/status \
  | python -c "import json,sys; print(list(json.load(sys.stdin)['limits']['licensedProfiles']))"
```

The acceptance tests read `DATAIKU_TEST_USER_PROFILE` and
`DATAIKU_TEST_USER_PROFILE_ALT` so you can point them at profiles your instance
actually has; they default to `DESIGNER` and `NONE`.

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

## Testing that connections actually connect

Most of the suite checks that DSS accepted a document. One test goes further and
asks DSS to dial the database a `dataiku_connection` describes, which is the
difference between a connection that parses and one that works.

It needs a database DSS itself can reach, so there is a Postgres service behind
a compose profile:

```bash
docker compose -f dev/docker-compose.yml --profile test up -d
```

Then point the test at it by service name — DSS resolves it on the compose
network, so nothing is published to the host:

```bash
export DATAIKU_TEST_PG_HOST=dss-test-postgres
make testacc
```

The other `DATAIKU_TEST_PG_*` variables (`_PORT`, `_DB`, `_USER`, `_PASSWORD`)
default to what the compose service uses. Without `DATAIKU_TEST_PG_HOST` the
test skips, so the suite stays runnable with no database.

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
