# 0003: deletion engine and path validation design

Status: done. This entry covers the design only; implementation is a separate, later step.

## Context

Phase 1 needed a concrete design for the two packages a wrong answer would hurt the most:
`internal/path-validation` and `internal/deletion-engine`. Both were specified before any code
was written, since this is the point in the project where a rushed decision is hardest to
undo later.

## Decision

Full detail lives in `docs/architecture/path-validation-design.md` and
`docs/architecture/deletion-engine-design.md`. Summarized:

- Path validation resolves paths one component at a time using descriptor-relative opens with
  symlink-following disabled at each step, and operates on the resulting file descriptor for
  every later syscall, rather than re-resolving a path string at the point of deletion. This
  removes a class of check-then-use race condition by construction, instead of defending
  against it with a second pass immediately before deletion.
- Structural safety (is this path safe to touch at all) is kept fully separate from policy
  (does a rule say this specific thing must never be touched). The former lives in
  path-validation, the latter in protection-rules.
- Deletion is split into a plan step, which produces an inspectable, hashed manifest, and an
  apply step, which accepts only a manifest, re-verifies each entry's identity before acting,
  and refuses if the manifest itself was altered since it was generated.
- Reversibility is the default for every destructive command, not an exception for one command.
  Deleted items move into a wtff-owned staging area with a retention window, not straight to
  permanent removal, and an undo command reads that staging manifest.
- Protection rules are declarative YAML, and every entry requires a reason and a source,
  addressing a specific, named gap in the tooling this project studied for research purposes
  before designing its own approach: a protection list with no stated provenance is hard for
  anyone but its original author to audit or extend correctly.
- Process-state checks return a three-value result, running, not running, or unknown, and
  unknown is treated as running, never as a convenient default.
- Privileged operations require every ancestor directory to be immutable to the invoking user
  before proceeding, and refuse outright rather than downgrade if that is not true.

## Rationale

These are the decisions with the highest cost if wrong, so they were written down and reasoned
through in full before any implementation began, rather than discovered incrementally while
writing code. The plan and apply split, and the provenance requirement on every protection
rule, are the two clearest points where this design tries to improve on what a purely
string-and-pattern-based validator can offer, and both were reasoned through here in enough
detail to be implementable.

## What was deliberately not done in this step

No code was written. This is a design document, appropriate for the model tier in use at the
time. Implementation of these two packages is safety-critical, high-blast-radius work and
belongs on the stronger model tier, per the routing rule in
`docs/architecture/model-routing.md`. That switch has not happened yet.

## How this could improve with time

Every open question listed at the end of both design documents needs an answer before or during
implementation, not after. The identity-snapshot tuple, the manifest serialization format, the
staging retention window, and the relationship between `clean --dry-run` and the plan step are
all flagged as unresolved rather than guessed at, and should be settled with the implementer
who is actually writing the code against real constraints, not decided in the abstract here.
