# 0002: repository identity and toolchain setup

Status: done.

## Context

The foundation laid out in decision 0001 was staged but not committed, no git identity existed
on this machine, and no Go toolchain was installed. All three had to be resolved before Phase 1
could begin.

## Decision

- Git identity is set repo-local, not global, so it does not affect other repositories on this
  machine: `user.email` is `leslie.musengi@icloud.com`. `user.name` was set to "Leslie Musengi",
  inferred from the email address since a name was not given explicitly; this is a guess and
  open to correction.
- The first commit, `3b9a291`, landed the full foundation from decision 0001.
- The Go toolchain, version 1.26.5, was installed via Homebrew, which was already present on
  this machine.

## An error caught during this step

Before staging the fix, a scan of every markdown file in the repository found em dash
characters in `README.md`, in the project layout list, despite an explicit standing rule
against them, recorded in both persistent memory and in `CLAUDE.md` in this same commit. They
were replaced with colons, since the surrounding text was a definition list and a colon reads
more naturally there than a hyphen. The corrected file was what actually landed in the first
commit, not the version with the mistake in it.

## Rationale

Repo-local git identity keeps this project's authorship separate from any other work on this
machine without requiring a global config change, which is the safer default when the two are
not asked to match. Catching the em dash mistake by scanning the actual files, rather than
trusting that the rule was followed, is the same verify-over-assume discipline this project's
own deletion engine is meant to embody. It would have been inconsistent to hold the code to
that standard while trusting prose without checking it.

## How this could improve with time

If `user.name` inferred from the email address turns out to be wrong, correct it and amend the
first commit only if it has not been pushed anywhere yet; once shared, prefer a follow-up commit
instead of rewriting history. Future documentation should be scanned for em dashes as a matter
of routine before it is written to disk, not caught after the fact by a dedicated scan.
