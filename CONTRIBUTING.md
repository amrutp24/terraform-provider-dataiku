# Contributing

## Getting set up

Requires Go (the version in `go.mod`) and Terraform 1.8 or later.

```bash
make build        # compile
make test         # unit tests, no DSS instance needed
make lint         # gofmt, go vet, golangci-lint
make docs         # regenerate docs/ from the schemas and examples/
```

The modules that stand up an instance live in their own repositories, since the
Terraform Registry derives a module's address from its repository name. Their
tests live with them.

`docs/` is generated. Edit the schema descriptions and the files under
`examples/`, then run `make docs` — CI fails if the committed output is stale.

## Testing

The suite runs against an in-process fake DSS by default, so `go test ./...`
needs no instance.

Point it at a real one by setting `DATAIKU_HOST` and `DATAIKU_API_KEY`, and the
same tests run against that instead. Do this before sending a change that
touches the API: the fake only knows what we believed the API does, and most
bugs found in this provider so far were field names that were wrong in ways only
a real instance revealed.

```bash
export DATAIKU_HOST=https://dss-scratch.example.com
export DATAIKU_API_KEY=...
make testacc
```

The tests create and delete real projects, users, groups and connections. Use a
scratch instance, never production. [dev/README.md](dev/README.md) explains how
to run one locally in Docker.

Which licence profiles exist varies by instance, so the user test reads
`DATAIKU_TEST_USER_PROFILE` and `DATAIKU_TEST_USER_PROFILE_ALT` if the defaults
do not suit yours.

## Adding a resource

1. Add the API calls to `internal/dataiku`. Updates should read the current
   document, change only the managed fields, and write it back — DSS replaces
   documents wholesale on `PUT`, and this is what stops an apply from wiping
   settings the provider does not model.
2. Add the resource to `internal/provider` and register it in `provider.go`.
3. Support `terraform import`.
4. Add acceptance tests, and teach `fakedss_test.go` enough to serve them.
5. Add an example under `examples/resources/<name>/` and run `make docs`.

Verify field names against a real instance rather than inferring them from
Dataiku's Python client. That client is a good guide to *paths*; it is not
reliable for the *shape* of what those paths accept.

Two patterns worth following, both learned the hard way:

- Where DSS owns part of a value — the group it attaches to every new user, say
  — model the attribute as the subset Terraform manages rather than as the whole
  truth, or apply will fail with an unexpected new value.
- Do not expose reverse references on a resource. A folder's contents are read
  before the projects that populate them exist; those belong on a data source.

## Commits

Explain why the change is needed, not just what it does. If it fixes something
subtle, say what the symptom was.
