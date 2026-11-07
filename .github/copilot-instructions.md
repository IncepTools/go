# inceptools instructions

Read `AGENTS.md` and `.agents/skills/inceptools/SKILL.md` before changing this
repository. Use the `icpt` CLI and existing framework packages, preserve the
controller -> service -> repository -> database boundaries, mirror tests under
`tests/`, and remember `examples/` is a separate Go module. Run focused tests,
the relevant root/examples checks, and `git diff --check`. Never add secrets or
perform destructive release, tag, migration, or publishing actions without
explicit user authorization.
