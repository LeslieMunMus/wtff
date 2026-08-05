# 0009: clean rule catalog and the clean command

Status: done.

## Context

The safety core and the `remove` primitive existed, but nothing proposed what to remove.
This entry covers `internal/clean-catalog`, the declarative source of candidate cache
categories, and `wtff clean`, the command that discovers and removes them.

## The design changed after empirical inspection, before any code was written for it

The original plan was one broad rule: treat `~/Library/Caches` as fair game, propose every
immediate child as a candidate, and let `internal/protection-rules` catch the exceptions.
Before writing that rule, the actual directory on this machine was inspected rather than
assumed. It contained over ninety entries under the `com.apple.` namespace, and several
without that namespace, including `FamilyCircle`, `CloudKit`, `GameKit` and
`AppSubscriptionsConfiguration`, holding family sharing, cloud sync, and subscription state,
not disposable content, despite living in a directory whose entire documented contract is that
its contents are safe to purge.

That changed the design before any catalog code was written. Third party applications
generally honor the Caches contract, since they are consumers of a published API. Apple's own
first party services do not reliably follow it for their own directories in the same tree. So
discovery excludes the `com.apple.` namespace under Caches by default, and
`internal/protection-rules` gained a new file, `apple-service-state.yaml`, protecting that
namespace and the specific named exceptions found during inspection, each with its own reason
and provenance recording that it came from looking at a real machine on this date, not from
a general assumption about what a cache directory contains.

## What was built

`internal/clean-catalog`: a schema mirroring `internal/protection-rules` in shape, opposite in
direction. Where protection rules answer "must this never be removed," the catalog answers
"where does reclaimable space usually live." Every entry requires the same reason and
provenance discipline the protection rules already enforce. Two entry kinds: `container`,
which enumerates a directory's immediate children as individual candidates, and `opaque`,
which treats an entire directory as one candidate because its internal layout is not
meaningful to review item by item, such as a content addressed cache store.

Five entries across three files: third party application caches under `~/Library/Caches`,
Xcode DerivedData, npm's content addressed cache, the XDG-style `~/.cache` many cross platform
tools use, and Trash contents.

`wtff clean [--dry-run] [--purge] [--yes]`: loads the catalog, discovers candidates against the
real home directory, plans and applies through the same engine and rule set `remove` uses,
sharing its confirmation, print, and reporting logic rather than duplicating it.

## Decisions worth recording

**Discovery never trusts itself.** Every candidate it proposes still passes through the
deletion engine's full path validation and protection rule check before anything happens to
it, exactly as if a person had typed the path directly into `wtff remove`. Discovery decides
what to suggest; the engine decides what is safe. An integration test exercises this
specifically: a fixture plants both a disposable third party cache and a directory shaped like
one of the found risk items, and asserts the engine removes only the first despite the catalog
proposing both as raw candidates before validation.

**The exclusion in discovery is a courtesy, not the safety boundary.** It exists so a `com.apple.`
prefixed directory does not generate a noisy skip line for every one of the ninety-some entries
found on a real machine. The actual authority is `internal/protection-rules`, checked
independently. A gap in the catalog's exclusion list would still be caught there. This was
proven, not just designed: every one of the five specifically named risk items reached the raw
candidate list from discovery, since only the broad namespace is excluded there, and every one
was still caught and explained by protection rules before anything happened to it.

**A category absent from a machine is not a failure, and is not shown.** Most categories will
not apply to most machines. `Skip` carries a `CategoryAbsent` field so a caller can tell "this
directory does not exist here" apart from "an item inside an existing directory was excluded,"
and only the second is worth a line of output.

## Two carve outs, added after the broad namespace rule broke an existing test

Adding the broad `com.apple.` protection immediately failed a protection rules test from
decision 0007, `TestShippedRulesDoNotOverreach`, which asserted Safari's WebKit cache must stay
reclaimable. That test was not wrong; the new rule was too broad for one specific, well
documented case. Safari's own Develop menu exposes an Empty Caches command that clears exactly
that directory, which is Apple's own confirmation that it is expected to be cleared. A narrow
`allow` rule carves it back out, following the same pattern the Docker credential carve out in
decision 0007 established: broad protection by default, narrow and individually justified
exceptions where the evidence is specific and strong. A test confirms the carve out is narrow,
not a blanket exemption for Safari's entire cache directory.

The remaining `com.apple.` namespace, including directories such as Xcode's own cache entry
that are plausibly also safe, stays protected. Only Safari's WebKit cache had evidence strong
enough to individually vet in this pass; the rest is an accepted, stated limitation rather than
an oversight.

## Two more protections added while reviewing real output, not during the original inspection

Running `wtff clean --dry-run` against a real machine surfaced `PassKit` and `askpermissiond`,
neither namespaced under `com.apple.` and neither flagged during the first inspection pass.
`PassKit` holds `PassAssetCache`, `RemoteCards` and a `cache.plist`, consistent with Apple
Wallet's local cache of pass artwork; `askpermissiond` is the system permission prompt daemon
and its directory holds a database named `Cache.db`. Neither was confirmed to hold anything
more sensitive than a rendering cache, and both were protected anyway: the reclaim value in
each case is a few megabytes at most, which does not outweigh the risk of being wrong about
something adjacent to payment or permission infrastructure. This is recorded as a finding made
by using the tool against a real system, not as something the original design review should
have caught, and it is the reason the review is described as bounded rather than exhaustive:
the remaining roughly eighty non-namespaced, unreviewed entries under Caches on this one
machine were not individually audited, and more will exist on other machines.

## Verification

Two hundred and eighty seven tests passing across the tree, `go vet` clean, `gofmt` clean.

Beyond the package test suites, the whole stack was run against this real machine repeatedly
during development, not only at the end: `wtff clean --dry-run` was inspected by hand multiple
times as the rule set changed, each time reading the actual proposed list rather than trusting
that a smaller test fixture generalized. A purge was run to completion against a planted,
disposable file inside a real catalog container, `~/.cache`, and confirmed removed rather than
staged. A non-interactive purge attempt against a piped session was confirmed refused, and an
earlier manual check of that same refusal was initially misread as a success because a `grep`
filter hid the "aborted" line; rereading the full output rather than trusting the filtered view
is what caught that misreading before it was reported as a finding.

## Known limitations

The review of what lives under `~/Library/Caches` is bounded, not exhaustive. It found and
addressed the entries with a clear signal, Apple namespacing or an obvious relation to a
sensitive system such as payments or permissions. It did not individually vet every third party
directory found there, and a different machine will have different applications and
potentially different risks that this pass did not see.

There is no per category or per item selection yet. `wtff clean` proposes everything the
catalog and the current machine produce together, in one plan; a person cannot ask for only
developer tool caches or exclude one category without editing the rule files. The plan output
is also a flat list with no grouping or pagination, which will read as a wall of text on a
machine with many caches; this is accepted for now as a correctness first, interface second
tradeoff, with the full screen terminal shell as the eventual place to solve it properly.

## How this could improve with time

Extend the Apple service state review incrementally as more risk signals are found, the same
way `PassKit` and `askpermissiond` were found: by using the tool against real machines and
reading real output, not by attempting a one time exhaustive audit. Add category selection once
the catalog has enough entries that removing everything at once stops being the common case.
Revisit whether other well documented, individually safe entries under the `com.apple.`
namespace deserve their own carve out, following the Safari precedent, one at a time with real
evidence for each.
