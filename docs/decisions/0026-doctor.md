# 0026: wtff doctor

Status: done.

A self diagnostic, available as `wtff doctor` and as `doctor` in the shell. It
exits non-zero when something needs a decision, so it can run from a scheduled
job that only wants to hear about problems.

## What earns a check

One rule decided the contents: a check has to describe something already
costing the person something or about to surprise them, and it has to say what
to do. A diagnostic reporting a number nobody can act on is decoration, and
decoration is what teaches people to stop reading diagnostics.

Five areas: what staging holds and how old it is, the audit log's size and
permissions, whether the compiled in rules and catalog load, whether any
catalog justification has gone stale, and whether Full Disk Access is granted.

Healthy areas are printed too. A diagnostic that speaks only when unhappy
leaves a person unable to tell a clean bill of health from a check that never
ran.

## Three findings worth their place

**Staging directories wtff cannot restore.** `ListBatches` skips a directory
whose record is missing or unreadable, silently and correctly: it may be a run
interrupted before it finished writing one, and the items are still there.
Silence is right for listing and wrong for a diagnostic, because this is
precisely the recoverable data nobody knows they have. Doctor names each one
and says the files are under `items/`.

**Batches held for a month.** Staging exists so a decision can be deferred, not
avoided. A recent batch is a note; one older than thirty days is a warning,
because that space is almost always held by accident.

**Full Disk Access.** Without it wtff cannot see parts of the home directory,
so it silently under reports. That is the safe direction to be wrong and a
confusing one to be wrong in silently, and it explains the otherwise baffling
case of a scan that finds less than a person expects. The probe reads
locations macOS withholds, confirmed denied on Darwin 25.6 from a terminal
without the grant, and it reports an honest "cannot tell" when none of them
exist rather than guessing.

## Red team

**Diagnostics must not disturb what they inspect.** This is the command a
person runs when they already suspect something is wrong, and the worst
possible behavior would be changing the evidence. A test walks the tree before
and after a run and requires it identical, including the staged batch.

**A mutation that did not compile, again.** Neutering `NeedsAttention` with a
constant left an unused loop variable, so the build failed and the result was
briefly mistaken for a pass. Rerun so it compiled, four tests failed.

Five checks were each disabled in turn and the matching test confirmed to fail:
orphan surfacing, the age escalation, both permission checks, the Full Disk
Access probe, and `NeedsAttention` itself, which is what the exit status means.

## Verification

Fourteen tests, full suite green. Run against the developer's real machine, it
correctly reported the 226.2MB batch actually staged there an hour earlier, and
correctly reported Full Disk Access as not granted, which that terminal does
not have. Against a fixture holding a planted orphan and a loosened staging
directory, both surfaces reported both problems and the command exited 1.

## Known limitations

Nothing checks whether the staging area sits on the same volume as the home
directory, which is the condition that makes staging fail with a cross volume
error. It is worth adding and needs a mounted second volume to test honestly.

The Full Disk Access probe infers from three locations. A machine where all
three are absent reports "cannot tell", which is honest but unhelpful, and
there is no way to distinguish a genuinely absent location from one hidden by a
different privacy control.
