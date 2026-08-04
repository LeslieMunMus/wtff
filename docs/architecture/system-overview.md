# System overview

Status: draft, to be revised as the deletion engine phase produces real decisions.

## Scope for version one

Two commands: `clean` and `uninstall`. Everything else (a disk analyzer, a live status
dashboard, the security-indicator scanner) is planned but out of scope until this core exists
and is tested.

## Component map

| Package | Responsibility |
|---|---|
| `cmd/wtff` | Entrypoint. Flag parsing and dispatch only. No business logic. |
| `internal/deletion-engine` | The single funnel every destructive operation passes through. Nothing deletes a file directly; everything calls into this package. |
| `internal/path-validation` | Traversal checks, ancestor symlink resolution, capability-based file opens. Answers only one question: is it structurally safe to touch this path. |
| `internal/protection-rules` | Declarative rules describing what must never be deleted, each with a stated reason and source. Answers a different question than path-validation: is this specific thing protected by policy, even if it would otherwise be structurally safe to touch. |
| `internal/uninstall-core` | Application discovery and leftover matching for the `uninstall` command. |
| `internal/operation-log` | Structured, append-only record of every operation wtff performs, for audit and for a future history command. |
| `internal/cli` | Command definitions and terminal output rendering for the non-full-screen paths (scripting, automation, `--json` output). |
| `internal/terminal-shell` | The full-screen interactive interface. See `terminal-interface.md`. |

## Why deletion is split into two separate concerns

Path validation and protection rules are deliberately two different packages answering two
different questions, rather than one combined check. Path validation is structural: is this an
absolute path, does it escape its intended root through a symlink, is the ancestor chain
mutable in a way that lets it be redirected after validation. Protection rules are policy: even
a structurally valid, unremarkable path can be something that must never be deleted, such as a
keychain file or an active database. Keeping these separate means a bug in one does not
silently weaken the other, and it means the protection-rule set can be extended, audited, and
tested independently of the lower-level path mechanics.

## Deletion flow, as currently planned

A caller never deletes a path directly. It requests deletion through the deletion engine, which
runs path validation, then checks protection rules, then (for `clean`) executes or (for
`uninstall`, and for any `clean` preview) stages the change and records it in the operation log.
This is a plan, not yet implemented; it will be revised once the deletion-engine phase begins
and real edge cases surface.

## Open questions

- Exact shape of the plan and apply split: whether every operation always goes through an
  explicit two-step plan then apply, or whether that is reserved for the interactive shell and
  a direct path exists for scripted use.
- Whether reversibility (a staged undo, not just a Trash move) is a version-one requirement or
  a fast-follow.
- Module path for `go.mod`, pending a decision on where this repository will eventually be
  hosted.

## How this could improve with time

This document should be rewritten, not just appended to, once the deletion engine has a working
implementation. A system overview that only describes intent starts to actively mislead once
real code exists and diverges from the plan in small ways.
