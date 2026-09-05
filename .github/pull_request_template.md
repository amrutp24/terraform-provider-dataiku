<!-- Explain why this change is needed, not only what it does. -->

## What this changes

## Why

## Testing

- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] `make docs` run, and the result committed if it changed

If this touches how the provider talks to DSS, say which instance it was run
against — version and edition. The fake DSS in the test suite only knows what we
believed the API does, so a change to an API call that has only been tested
against the fake has not really been tested.

- DSS version and edition tested against:
