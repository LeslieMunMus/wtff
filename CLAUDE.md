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

There is no build yet. Once `go.mod` exists, this section will list the commands to run before
any change is considered complete (build, vet, test, lint).
