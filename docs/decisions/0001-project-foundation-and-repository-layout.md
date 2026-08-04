# 0001: project foundation and repository layout

Status: done.

## Context

Before any deletion-engine work begins, the project needed a settled home directory, a naming
convention, a documentation habit, and a locked terminal-interface requirement, so that later
work has somewhere consistent to land and so that architectural decisions are not lost between
sessions.

## Decision

- The project root is `~/wtff`. This directory already existed with two reference folders
  (`need-to-know`, screenshots of an earlier planning discussion, and `theme-colours`, a design
  reference image) and both already complied with the naming convention, so they were left in
  place rather than reorganized.
- The repository layout is: `cmd/wtff` for the entrypoint, six packages under `internal/`
  (`deletion-engine`, `path-validation`, `protection-rules`, `uninstall-core`,
  `operation-log`, `cli`, `terminal-shell`), `docs/architecture` for living technical
  documentation, `docs/decisions` for a dated record of what was decided, `test` for the test
  suite, and `scripts` for build and release helpers.
- Naming convention: kebab-case for everything created freely, with named exceptions for
  filenames a toolchain requires by exact spelling. Full rule in
  `docs/architecture/naming-conventions.md`.
- Model routing: Opus for safety-critical work, Sonnet for volume work, no silent tier
  switching. Full rule in `docs/architecture/model-routing.md`.
- Terminal interface: full-screen, in the style of Claude Code's own CLI, locked in now even
  though implementation is a later phase. Full rule in
  `docs/architecture/terminal-interface.md`.
- Documentation habit: every completed step is recorded here, numbered and dated in filename
  order, with what was done, why, and how it could improve, and a status marked with the word
  "done."

## Rationale

A repository that only accumulates code without a parallel record of why each structural
decision was made becomes expensive to reason about later, both for a future session picking
the work back up and for anyone outside the project trying to evaluate it. Writing this down
now, while the decisions are still small and easy to state precisely, costs little. Writing it
down later, after the reasons have been forgotten or entangled with other changes, costs much
more.

## What was deliberately not done in this step

- No `go.mod` yet. The module path depends on where this repository will eventually be hosted,
  which has not been decided, and creating one with a placeholder path risks having to
  rewrite every import later.
- No license file. The repository defaults to all rights reserved until a license is chosen.
- No git commit yet. The repository was initialized, but the first commit is pending
  confirmation of the name and email to attribute commits to.
- No Go toolchain installed. `go` was not found on this machine. Installing it (most likely via
  Homebrew, which is already present) is a system change and will be proposed explicitly before
  it happens, rather than done as a side effect of documentation work.

## How this could improve with time

Once the module path is settled, this entry should get a short addendum noting the final
`go.mod` path and the date it was added, rather than opening a new decision entry for what is
really a continuation of this one.
