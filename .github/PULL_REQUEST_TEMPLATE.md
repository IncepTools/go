## What this changes

<!-- One or two sentences. The diff says how; say why. -->

## Why

<!--
If this fixes a defect, describe the failure: the inputs, and what happened
instead of what should have. That sentence usually belongs in the test and in
the code comment too.
-->

## Checklist

- [ ] `make ci` passes (`tidy-check`, `fmt-check`, `vet`, `test -race`)
- [ ] A test covers the behaviour, and fails without the change
- [ ] Exported identifiers have doc comments saying *why*, not restating the signature
- [ ] Behaviour changes are reflected in the README and in `docs/`
- [ ] `CHANGELOG.md` has an entry under Unreleased

## Compatibility

- [ ] No breaking change to an exported API
- [ ] Breaking change, described here with the migration:

<!-- Before / after -->
