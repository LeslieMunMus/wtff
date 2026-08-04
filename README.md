# wtff

wtff (What The Floating Files) is a terminal-first macOS maintenance toolkit. It is an
independent, from-scratch project. It is not a fork, port, or derivative of any existing
cleanup tool, and it does not reuse code, comments, or curated data from any other project.

## Status

Foundation phase. No user-facing commands exist yet. See `docs/decisions/` for what has been
decided and `docs/architecture/` for how the system is meant to fit together.

## Scope for version one

Two commands only: `clean` and `uninstall`. A disk analyzer and a live status dashboard are
planned for a later version and are intentionally out of scope until the deletion core is
solid and tested.

## Why this exists

Most macOS cleanup tools ask you to trust a black box: either closed-source commercial
software, or open-source shell scripts with hardcoded protection rules and no stated
provenance. wtff is built around three commitments instead:

- Every deletion goes through one auditable path, never an ad hoc `rm -rf`.
- Every protection rule carries a reason and a source, not just a pattern.
- Every destructive action can be previewed before it runs and undone after it runs.

## Project layout

- `cmd/wtff`: the entrypoint binary. Flag parsing and dispatch only, no business logic.
- `internal/deletion-engine`: the single funnel every destructive operation must pass through.
- `internal/path-validation`: traversal checks, symlink resolution, capability-based opens.
- `internal/protection-rules`: declarative, provenance-tagged rules describing what must
  never be deleted.
- `internal/uninstall-core`: application discovery and leftover matching for `uninstall`.
- `internal/operation-log`: structured audit log of every operation wtff performs.
- `internal/cli`: command definitions and terminal output rendering.
- `internal/terminal-shell`: the full-screen terminal interface.
- `docs/architecture`: living technical documentation describing how the system works.
- `docs/decisions`: a dated record of what was decided, why, and what could improve.
- `test`: the test suite, including adversarial path-validation cases.
- `scripts`: build, lint, and release helpers.

## Naming convention

See `docs/architecture/naming-conventions.md`.

## License

Not yet chosen. Treat the repository as all rights reserved until a license file is added.
