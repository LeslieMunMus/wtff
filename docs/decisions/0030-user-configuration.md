# 0030: user configuration

Status: done.

wtff reads `~/.config/wtff`, honouring `XDG_CONFIG_HOME`. Rules go in
`rules/`, cleanup categories in `catalog/`, both in the same schema as the
built in files.

XDG rather than Application Support, decided with the project manager: these
files are meant to be edited by hand and kept in a dotfiles repository, and
Application Support is convenient for neither. wtff's own state, which nobody
edits, stays where macOS expects it.

## The question this turned on

Whether a user rule may carve an exception out of a built in protection.

Decided yes, with two limits, also with the project manager. The machine's
owner is entitled to overrule a judgement call wtff made on their behalf, and a
tool that offers no way to do it leaves someone stuck behind a rule that is
merely too broad. But the value of a protection list is that it can be trusted
without being read, and an unannounced local override spends exactly that.

**Critical protections cannot be carved.** Credentials, keychains, and
irreplaceable personal data are marked critical precisely so that no local
configuration reaches them. The existing evaluator already ignored such a carve
out, since a critical protection wins regardless of specificity, but ignoring is
the wrong response: a rule that quietly does nothing lets a person believe it
works. It is refused when the configuration loads, and the refusal names the
rule it collided with.

**Every override is announced.** Before the plan, on every command that
removes anything, and again from `wtff doctor`. A notice arriving after a path
has been removed is not a warning, it is a receipt.

Nothing else needed inventing: the merged set is evaluated by the precedence
already in the package, where a more specific rule wins and a critical
protection wins over everything.

## What configuration may not do

**Authorise irreversible removal.** A user catalog entry cannot mark itself
purgeable. A built in purgeable entry carries a justification argued in this
repository and reviewable by anyone; a local file marking something purgeable
would be a permanent deletion authorised by a line nobody else ever reads.
`clean` will stage such an entry, reversibly, which is the same outcome one
step further from regret.

**Remove a built in category.** There is no subtraction, and that is not an
oversight. A catalog entry only proposes candidates, and every one still passes
the protection rules and the structural floor. Something a person wants left
alone belongs in a protection rule, which is the layer that answers that
question, rather than in a subtraction from a list of suggestions.

**Reuse a built in identifier**, in either file, since two rules answering to
one name in the log helps nobody.

**Skip provenance.** User files are held to the same schema. A local rule
nobody can account for later is the same maintenance problem the requirement
exists to prevent.

A configuration that does not load stops the command outright rather than
falling back to the built in set. Running with fewer protections than a person
believes they have is the one failure this project refuses to let happen
quietly.

## Red team

**Doctor ignored the configuration.** It reported the built in counts and
nothing else, which meant the one command whose job is saying what state wtff
is in was silent about the files most likely to have changed that state. It now
reports where configuration was loaded from, every override, and a
configuration that fails to load, which is where a person will look when every
command starts refusing to run.

**Two tests asserted the wrong thing, both by choosing a path already
protected.** One expected a user rule to be the responsible rule for
`~/Documents/thesis`, which a built in already covers; the path was protected,
just not by the named rule. The other tried to demonstrate an allowed override
against `~/Documents`, which is critical, and was correctly refused. Both were
corrected to paths the built in set does not reach, and the second is now
demonstrated against `cloudkit-sync-state`, which is genuinely standard.

Five guards were each removed in turn and the matching test confirmed to fail:
the critical override refusal, checking every expanded pattern rather than the
first, reserved built in identifiers, the override report, and the refusal of a
user entry marking itself purgeable.

## Verification

Full suite green. Against the installed binary: a user catalog entry added a
category to `clean`; a carve out aimed at the login keychain was refused when
the configuration loaded, naming `user-keychain-directory`; a carve out against
`cloudkit-sync-state`, a standard rule, was allowed and announced before the
plan; and `doctor` reported both the configuration and the override.

## Known limitations

Overrides are computed by evaluating each user carve out's own patterns, so an
override that only takes effect for a path neither rule names literally will
not be reported. Widening that means evaluating candidate paths rather than
rule patterns, which cannot be done before a scan.

There is no `config.yaml` for preferences. Defaults such as always running dry
first are not configurable, and every flag is still per invocation.

Nothing validates that a user rule is doing something useful. A rule protecting
a path that does not exist loads happily and protects nothing.
