# Deletion engine: design

Status: design done, implementation not started.

## What this package is responsible for

The single funnel every destructive operation passes through. No other package in wtff deletes,
moves, or truncates a file directly. Everything else, including `clean` and `uninstall`, asks
this package to do it, and this package is the only place that consults
`internal/path-validation` and `internal/protection-rules` before acting.

## Plan and apply, as two separate steps

Every destructive operation is split into a plan step and an apply step, and the apply step
only ever accepts a plan, never a raw list of paths.

**Plan** takes a set of candidate targets, produced by `clean` rule matching or by
`internal/uninstall-core` discovery, and for each candidate: runs structural path validation,
runs protection-rule evaluation, measures size, and records which rule or discovery evidence
included it. The output is a manifest: an ordered, inspectable list of exactly what would
happen, with a reason attached to every entry, plus a hash of the manifest's own contents.

**Apply** takes a manifest, not a path list, as its only input. Before touching anything, it
recomputes the manifest's hash and refuses to proceed if it does not match what was reviewed,
so a plan cannot be silently edited between generation and execution. For each entry, it
re-checks the identity captured during planning (device and inode, matching what
`internal/path-validation` snapshotted) against the current state of that path. An entry whose
identity has changed is skipped and logged, never forced through. Only entries that still match
are executed.

This gives two things a single combined plan-and-delete step cannot: a manifest the user (or
the interactive terminal shell) can actually review before anything happens, and a hard
guarantee that apply operates on what was planned, not on whatever happens to be at that path
by the time apply runs.

## Reversibility

`clean` in most existing tools is permanent by default; only a separate command like an
uninstaller or a disk browser routes through the Trash. wtff makes reversibility the default for
both, not an exception carved out for one command.

Deleted items move into a wtff-owned staging area first, not directly to their final fate. The
staging area is a dedicated directory in the user's application-support tree, not the system
Trash, because the system Trash has its own quirks (some app bundles cannot be moved there even
after authorization) and because a dedicated staging area lets wtff track its own expiry policy
and its own manifest of what was staged, why, and when it will be purged. Same-volume moves use
a rename; cross-volume moves fall back to a copy followed by a source delete, since a rename
cannot cross a volume boundary.

An `undo` command reads the staging manifest and restores an entry to its original path,
provided the original location has not since been reoccupied by something else. Staged items
are purged automatically once they pass their retention window, which is where the actual
space gets reclaimed; reversibility is a delay, not a permanent hold.

Permanent, non-staged deletion is a separate, explicit mode, not the default, for the narrow
cases where staging is not meaningful, such as an item that is not on a writable volume wtff
can stage into.

## Protection rules: declarative, provenance-tagged

Rules describing what must never be deleted live as data, not as logic, in
`internal/protection-rules`, loaded from a rule file rather than compiled into a chain of
conditionals. A working shape for one entry:

```yaml
- id: keychain-directory
  path_pattern: "~/Library/Keychains"
  match: prefix
  reason: "Contains user credential material. Never a cleanup target regardless of size or age."
  provenance: "Apple Platform Security Guide, Keychain Services documentation"
  category: user-data
  added: 2026-08-05
```

Every entry carries a reason and a source. This is the direct answer to a real gap in existing
tools of this kind: a large, hardcoded protection list with no stated reason for any individual
entry is hard to audit, hard to extend correctly, and hard to trust. A rule file where every
entry says why it exists and where that justification came from can actually be reviewed by
someone other than the person who wrote it.

## Process-state awareness

Deleting an active application's cache or state file out from under it can corrupt that
application's data. Before deleting anything associated with a running process, the engine
needs to know whether that process is running, and needs a third answer, not just yes or no,
for when that cannot be determined reliably. A check that can only return running or not-running
will, on some failure of the underlying process inspection, silently collapse into whichever
answer the code happens to default to, and if that default is "not running," a live process's
files get deleted. The engine instead treats process state as a three-value result: running,
not running, or unknown, and unknown is treated the same as running, denying the deletion,
never treated as a "probably fine" default.

## Privileged operations

Any deletion or move that requires elevated privilege first verifies that every ancestor
directory in the resolved path is immutable to the invoking, non-privileged user: owned by
root, without group- or world-write bits set, not a symlink, and without a permissive ACL entry.
Without this check, a non-root owner of some ancestor directory could alter it between the time
a privileged operation is authorized and the time it actually runs, redirecting where a
privileged delete or move actually lands. If any ancestor fails this check, the operation is
refused, not downgraded to a weaker privilege level.

## Audit logging

Every plan generated and every apply executed is written to `internal/operation-log` as a
structured record: what was planned, what was actually applied, what was skipped and why, and
what failed and why. This is the data source for a future `history` command and for the
`undo` command's ability to find what it staged.

## Open questions for implementation

- Exact manifest serialization format and hashing scheme.
- Retention window for staged deletions, and whether it is a fixed default or user-configurable.
- Whether plan generation itself needs a timeout budget, given that some candidate sets (a full
  developer-tools sweep, for instance) could be large enough that plan generation itself takes
  meaningful wall-clock time.
- How `clean`'s dry-run mode relates to the plan step: whether dry-run is simply "generate a
  plan and stop," which would mean the two are naturally the same mechanism rather than two
  separate code paths.

## How this could improve with time

The plan/apply split and the staging-based undo are the two biggest structural bets in this
design, and both are unproven until real implementation and real adversarial testing exist.
This document should be revisited and corrected once that testing surfaces cases the design did
not anticipate, rather than treated as a fixed contract the implementation must bend to fit.
