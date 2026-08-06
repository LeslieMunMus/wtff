# wtff

wtff (What The Floating Files) is a terminal-first macOS maintenance toolkit. It is an
independent, from-scratch project. It is not a fork, port, or derivative of any existing
cleanup tool, and it does not reuse code, comments, or curated data from any other project.

## Status

The safety core, every planned command, and the full-screen interactive shell are built and
tested.

## Install

```bash
make install
```

That builds with checks, stamps the version from git, and installs `wtff` into
`$(go env GOPATH)/bin`. If that directory is not on your `PATH`, add it to `~/.zshrc`, which is
the file an interactive zsh actually reads on macOS:

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
```

Open a new terminal and `wtff` works from anywhere, arguments included. `make run` builds and
launches from the source tree instead, for the edit and try loop.

```bash
wtff                                              # full-screen interactive shell
wtff clean --dry-run                              # preview reclaimable caches, changes nothing
wtff clean                                        # stages them, reversible
wtff uninstall --dry-run "App Name"               # preview an app and its leftovers
wtff uninstall "App Name"                         # stages it, reversible
wtff remove --dry-run ~/Library/Caches/some-app   # preview a specific path
wtff purge --dry-run                              # preview emptying the Trash, permanent
wtff staged                                       # list what is staged
wtff undo <batch-id>                              # restore it
wtff staged --purge <batch-id>                    # delete it permanently instead
```

Implemented: structural path validation, the deletion engine with plan and apply, staging based
undo, the operation log, the protection rule schema with an initial rule set, a candidate
discovery catalog for common cache categories, application discovery and exact-evidence leftover
matching, `clean`, `uninstall`, and `remove` commands that exercise all of it end to end, and a
full-screen interactive shell covering Clean, Uninstall, and Staged as an alternative to the
scriptable commands.

`uninstall` matches by exact name or bundle identifier only, refuses Apple's own applications
outright, and has no vendor-specific protected list yet for software such as VPN clients or
endpoint security agents. The interactive shell can now delete permanently as well as stage, gated behind typing a
confirmation word.

See `docs/decisions/` for a numbered record of what was decided and why, and
`docs/architecture/` for how the pieces fit together. Every package documents itself in its own
`doc.go`.

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

MIT. See `LICENSE`.

wtff is an independent, from-scratch project. It does not reuse code, comments, or curated data
from any other cleanup tool. Architectural ideas and publicly documented facts about macOS are
not copyrightable; specific expression, including a curated protection list as a compilation, is.
Every protection rule and catalog entry in this repository carries a provenance field naming the
primary source it was derived from, so that claim can be checked rather than trusted.
