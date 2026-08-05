# 0007: protection rules schema and initial rule set

Status: done.

## Context

The deletion engine was built against a policy interface and had been running against a stub
that protects nothing. This entry covers the real thing: a schema for declarative protection
rules, a loader that validates them, a matcher with defined precedence, and a first rule set.

## The problem this schema is designed against

A protection list is the part of a cleanup tool nobody reviews until it is wrong. Entries
accumulate, the reasoning behind each one lives in the head of whoever added it, and after a few
years no one can say whether a given line is load bearing or cargo. That is both a maintenance
problem and a safety problem: an entry nobody understands cannot be safely narrowed when it turns
out to be too broad, and cannot be confidently trusted when it looks too narrow.

So the schema requires every rule to carry a reason and a provenance record naming the source of
that reason, and the loader refuses a rule that lacks either. Not as a documentation convention,
which would erode: as a load time requirement, enforced by a test that walks every shipped rule.

## Decisions worth recording

**Rules are data, embedded in the binary.** They ship inside the executable so a single
downloaded file is complete. A tool that reads its safety rules from a file beside itself fails
in the least helpful way available: it runs, finds nothing, and protects nothing.

**An empty rule set is refused.** It means everything is unprotected, which is never a correct
state to start from and is exactly what a packaging mistake that drops the rule files produces.

**Carve outs exist, and are the reason the schema has an effect field.** A tool directory
commonly holds a credential beside a genuinely rebuildable cache. Protecting the whole directory
puts the credential out of reach at the cost of never reclaiming the cache; protecting nothing
reclaims the cache and eventually loses the credential. Carve outs let both be true.

**Critical severity cannot be overridden by any carve out, at any specificity.** This is what
stops a narrow exception written for one purpose from exposing a credential store by accident of
precedence. A carve out marked critical is refused at load time, because it is a contradiction
and accepting it would leave its meaning to whatever the matcher happened to do.

**Precedence is most specific wins, with ties going to protection.** Ties are not hypothetical:
a protect and an allow rule written over the same path is how a disagreement between two rule
files presents, and resolving toward keeping data is the recoverable direction to be wrong in.

**Specificity is computed from the declared pattern, not the expanded one**, so rule precedence
does not vary with the length of a particular machine's home directory.

**A carve out that lost only because the protection was critical is named in the decision.**
Somebody wrote that carve out expecting it to apply, and a silent override is the hardest kind of
rule mistake to find.

**Patterns are stored in every form a resolved path might present**, including the form produced
by a home directory reached through a link. This is the same defect found in the deletion
engine's self protection during the previous review pass, applied here before it could occur
again rather than after.

## The initial rule set

Thirty one rules across five files: credentials and keys, personal data, cloud and sync, platform
state, and developer state.

Provenance is honest about method. Rules whose paths were confirmed to exist on the machine this
was developed on are marked as system inspection and name the operating system build. Rules for
paths that were not present, such as the SSH and GnuPG directories, are marked as documentation
and name the manual or vendor page they rest on, because claiming inspection for something not
inspected would make the whole provenance field worthless.

Synchronized storage is called out as its own category because it carries a hazard local data
does not: a deletion propagates to every other device on the account and to collaborators on
shared folders, usually before anyone notices, and a local undo cannot reverse that.

## Verification

Two hundred and fifteen tests passing across the tree, `go vet` clean, `gofmt` clean.

The suite walks every shipped rule and requires a substantive reason, a provenance source, a
readable verification date, a category, and at least one absolute expanded pattern. It checks
thirteen distinct ways a rule file can be malformed and requires each to be refused, with a
control case proving the valid form still loads.

Two tests exist specifically to catch over-protection, which is the failure mode a protection
system tends toward and which nothing else would surface: one walks a list of ordinary
reclaimable targets and fails if any is protected, the other probes every shipped rule with a
path built from its own pattern and fails if the rule cannot match anything.

An integration test in the deletion engine exercises the real rule set through a full plan and
apply, confirming a keychain survives while a cache file beside it is staged, and a compile time
assertion pins that the rule set satisfies the engine's interface.

The behavior was also checked directly against real paths rather than only through test names:
thirty one rules load, credentials and personal data are protected, ordinary caches are not, and
the carve out permits a docker cache while the credential file inside the same protected
directory stays critical.

## Red team findings

**The critical rule short circuit depends on load order.** Evaluate returns the first critical
protection it encounters, which is the most specific one only because rules are sorted by
specificity when they load. That coupling is invisible at the point it matters. A test now pins
it, so removing or changing the sort fails loudly instead of quietly reporting the wrong rule.

**Glob matching walks ancestors, which needed a boundary test.** A glob covers the directory it
names and everything beneath it, so that a rule for a sandbox Documents directory does not need a
second entry for its contents. The risk is leaking upward, protecting the parents of the match.
Tested in both directions.

**Wildcards must not span separators.** A pattern naming one level would otherwise silently
cover an arbitrarily deep tree. Tested.

## Known limitations

An absolute pattern written out in full outranks an equivalent home relative one, because the
tilde counts as one character when specificity is computed. Rules in this project are written
home relative for that reason, and a test documents the behavior so it is visible if the
convention is broken.

Case folding a glob would also fold any character class inside it. No shipped rule uses one, and
this is recorded rather than fixed.

There is no user supplied rule file yet. Only the built in set loads. Adding one raises a
question this entry does not settle: whether a user carve out should be able to override a
standard protection, and how to keep that from becoming a way to disable safety by accident.

The set is deliberately conservative and incomplete. It covers categories where the reasoning is
clear and the provenance is real. Preference files, application support data, and the many
vendor specific cases are not yet covered, and each needs its own justification rather than a
bulk import.

## How this could improve with time

Add rules as real cases justify them, one at a time with provenance, rather than in bulk. Add a
command that prints why a given path is or is not protected, since the decision already carries
the rule and reason and a person debugging a surprising skip should not have to read the rule
files. Revisit the verified dates periodically, because a provenance record with a stale date is
a claim that has stopped being checked.
