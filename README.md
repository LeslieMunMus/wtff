# wtff

wtff (What The Floating Files) is a terminal-first macOS maintenance toolkit. It is an
independent, from-scratch project. It is not a fork, port, or derivative of any existing
cleanup tool, and it does not reuse code, comments, or curated data from any other project.

## Status

The safety core, every planned command, and the full-screen interactive shell are built and
tested.

## Install

With Homebrew, once the tap exists:

```bash
brew install lesliemusengi/wtff/wtff
```

The tap builds from source on your machine. That is deliberate rather than
lazy: a prebuilt binary downloaded from the internet is quarantined by
Gatekeeper and will not open until it is notarized, which needs a paid Apple
Developer account, or cleared by hand with `xattr`, which teaches people to
disarm the protection that would have caught a genuinely malicious download.
Nothing arrives already built, so nothing is quarantined.

From a clone:

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

## Configuration

Optional, and most machines will never need it. wtff reads `~/.config/wtff`,
honouring `XDG_CONFIG_HOME` when set.

```
~/.config/wtff/rules/*.yaml      your own protection rules
~/.config/wtff/catalog/*.yaml    your own cleanup categories
```

Both use the same schema as the built in files, provenance included, and both
are additive: your entries join the built in ones rather than replacing them.

A rule may carve an exception out of a built in protection when it is more
specific, which is how you overrule a judgement call wtff made on your behalf.
Two limits on that. Rules marked critical, which is credentials, keychains, and
irreplaceable personal data, can never be carved: an attempt is refused when
the configuration loads rather than ignored later, so you find out rather than
assume it worked. And every override is announced before anything is planned,
and again by `wtff doctor`, because the value of a protection list is that it
can be trusted without being read.

A catalog entry you add cannot mark itself purgeable. `clean` will stage it,
reversibly. Permanent removal stays with entries whose justification is argued
in this repository where anyone can read it.

## Shell completion

```bash
wtff completion zsh > /usr/local/share/zsh/site-functions/_wtff
```

Start a new shell and tab completion offers commands, flags, staged batch
identifiers, and installed application names. For bash:

```bash
wtff completion bash > ~/.wtff-completion.bash
echo 'source ~/.wtff-completion.bash' >> ~/.bash_profile
```

## Releasing

```bash
make dist
```

Builds a universal binary for Apple silicon and Intel, archives it, and writes
a SHA-256 checksum. It depends on `make check`, so a release cannot be cut from
a tree that does not pass its own tests.

Tagging is what publishes. Pushing a `v*` tag runs the release workflow, which
rebuilds from a clean checkout, asserts the binary reports the tag it was built
from, and opens a draft release with the archive and checksums attached. The
draft is deliberate: it is a chance to read the notes before anyone else does.

```bash
git tag -a v0.1.0 -m "wtff 0.1.0"
git push origin v0.1.0
```

Then update `url` and `sha256` in `packaging/homebrew/wtff.rb`, and copy it to
`Formula/wtff.rb` in a repository named `homebrew-wtff`.

## License

MIT. See `LICENSE`.

wtff is an independent, from-scratch project. It does not reuse code, comments, or curated data
from any other cleanup tool. Architectural ideas and publicly documented facts about macOS are
not copyrightable; specific expression, including a curated protection list as a compilation, is.
Every protection rule and catalog entry in this repository carries a provenance field naming the
primary source it was derived from, so that claim can be checked rather than trusted.
