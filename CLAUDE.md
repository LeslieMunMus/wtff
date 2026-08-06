# Agent Guide

This file is the shared source of truth for any AI agent working on wtff. Read it before
making changes. It is intentionally short. Detailed technical decisions live in
`docs/architecture/` and `docs/decisions/`, not here, so this file stays cheap to reload.

## What wtff is

A terminal-first macOS maintenance toolkit, currently limited in scope to `clean` and
`uninstall`. See `README.md` for the project layout and `docs/architecture/system-overview.md`
for how the pieces fit together.

## Origin and licensing constraint

wtff was designed after a full read of an existing GPL-3.0 macOS cleanup tool, for research
purposes, to understand what a mature safety architecture in this space looks like. wtff does
not copy code, comments, or curated data from that project or any other. Architectural ideas
and publicly documented facts about macOS are not copyrightable and are fair game. Specific
expression, including curated protection lists as a compilation, is not. When in doubt, treat
it as off limits and re-derive from primary sources (Apple documentation, vendor documentation,
live system inspection) instead.

Do not reintroduce that project's source into this repository or this conversation's working
context. Rules and protection data must be re-derived from primary sources with a provenance
field on every entry.

## Standing rules

- No em dashes anywhere: not in code, comments, documentation, or chat responses. Use commas,
  periods, or single hyphens instead.
- Naming convention: kebab-case for every file and directory we create, with the exception of
  filenames a toolchain requires by exact spelling (`go.mod`, `README.md`, `LICENSE`,
  `.gitignore`, files under `.github/workflows/`). See `docs/architecture/naming-conventions.md`.
- Every completed step gets documented before moving on: what was done, why it was done that
  way, and how it could improve with time. Status is marked with the word "done," never an
  emoji. Completed work lives in `docs/decisions/` as a dated, numbered entry.
- Every stage gets a red team pass before it is called done. Attack the work that was just
  built, looking for what the design assumed rather than proved: comments asserting a property
  the code does not actually have, guards that a specific input shape walks around, windows
  between a check and the action it authorizes, and error paths that leak or fail open. Findings
  are recorded in the decision entry for that stage, including the ones that turned out to be
  wrong, and each real finding gets a regression test that would have caught it. Catching
  defects at the stage that introduced them is far cheaper than finding them later, when other
  work already depends on the broken behavior.
- Verify claims rather than asserting them. A passing test whose name describes a property is
  not evidence the property holds; confirm independently that the test fails when the protection
  is absent, or demonstrate the underlying behavior directly. Several tests in this project were
  found to pass for the wrong reason.
- The terminal interface must be a full-screen application, in the style of Claude Code's own
  CLI, not a scrolling line-by-line script. See `docs/architecture/terminal-interface.md`.
- Model routing is Opus for safety-critical, high-blast-radius work (the deletion engine, path
  validation, the capability layer, privilege-escalation guards, protection-rule schema design,
  and any security review) and Sonnet for everything else (CLI plumbing, serializers, test
  scaffolding, rule-file authoring, documentation, refactors). Never switch model tier without
  telling the user what is about to be built and why it needs that tier, and waiting for
  explicit agreement. See `docs/architecture/model-routing.md`.
- Deletion safety comes before feature velocity. The deletion engine and path validation are
  the long pole in this project on purpose and should not be rushed to unblock later phases.

## Verification

Run before any change is considered complete:

```bash
make check          # gofmt, vet, the full suite, and the em dash scan
```

`make help` lists every target. `make install` puts `wtff` on your PATH.

Never claim a fix or a property holds because a test with a matching name passes. Confirm the
test actually exercises the defect: run it against the code before the fix and see it fail, or
demonstrate the underlying behavior directly (a small standalone program, a manual repro against
the compiled binary). This project has caught three tests that passed for a reason other than
their name claimed, and the pattern that catches it is checking, not writing a more careful test
name.
