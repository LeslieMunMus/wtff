# wtff

wtff (What The Floating Files) is a terminal-first macOS maintenance toolkit. It is an
independent, from-scratch project. It is not a fork, port, or derivative of any existing
cleanup tool, and it does not reuse code, comments, or curated data from any other project.

## Status

The safety core is built and tested, and is now reachable from a real terminal command.

```bash
go build -o wtff ./cmd/wtff
./wtff clean --dry-run                              # preview reclaimable caches, changes nothing
./wtff clean                                        # stages them, reversible
./wtff uninstall --dry-run "App Name"               # preview an app and its leftovers
./wtff uninstall "App Name"                         # stages it, reversible
./wtff remove --dry-run ~/Library/Caches/some-app   # preview a specific path
./wtff remove ~/Library/Caches/some-app             # stages it, reversible
./wtff staged                                       # list what is staged
./wtff undo <batch-id>                              # restore it
```

Implemented: structural path validation, the deletion engine with plan and apply, staging based
undo, the operation log, the protection rule schema with an initial rule set, a candidate
discovery catalog for common cache categories, application discovery and exact-evidence leftover
matching, and `clean`, `uninstall`, and `remove` commands that exercise all of it end to end.

Not yet built: the full-screen terminal interface. `uninstall` matches by exact name or bundle
identifier only, refuses Apple's own applications outright, and has no vendor-specific protected
list yet for software such as VPN clients or endpoint security agents.

See `docs/decisions/` for a numbered record of what was decided and why, and
`docs/architecture/` for how the pieces fit together. Packages that are implemented document
themselves in their own `doc.go`; the remaining `README.md` files mark packages that are still
placeholders.

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
